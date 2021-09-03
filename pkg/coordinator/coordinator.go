/*
 * Tencent is pleased to support the open source community by making TKEStack available.
 *
 * Copyright (C) 2012-2019 Tencent. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use
 * this file except in compliance with the License. You may obtain a copy of the
 * License at
 *
 * https://opensource.org/licenses/Apache-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OF ANY KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations under the License.
 */

package coordinator

import (
	"context"
	"github.com/pkg/errors"
	"math"
	"time"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/wait"

	"github.com/sirupsen/logrus"
)

// Option indicate all coordinate arguments
type Option struct {
	// MaxSeries is max series every shard can assign
	MaxSeries int64
	// MaxShard is the max number we can scale up to
	MaxShard int32
	// MinShard is the min shard number that coordinator need
	// Coordinator will change scale to MinShard if current shard number < MinShard
	MinShard int32
	// MaxIdleTime indicate how long to wait when one shard has no target is scraping
	MaxIdleTime time.Duration
	// Period is the interval between every coordinating
	Period time.Duration
	// RebalancePeriod is the interval between every rebalance loop
	RebalancePeriod time.Duration
}

// Coordinator periodically re balance all replicates
type Coordinator struct {
	log              logrus.FieldLogger
	reManager        shard.ReplicasManager
	option           *Option
	concurrencyLock  chan int
	getConfig        func() *prom.ConfigInfo
	getExploreResult func(hash uint64) *target.ScrapeStatus
	getActive        func() map[uint64]*discovery.SDTargets

	lastGlobalScrapeStatus map[uint64]*target.ScrapeStatus
}

// NewCoordinator create a new coordinator service
func NewCoordinator(
	option *Option,
	reManager shard.ReplicasManager,
	getConfig func() *prom.ConfigInfo,
	getExploreResult func(hash uint64) *target.ScrapeStatus,
	getActive func() map[uint64]*discovery.SDTargets,
	log logrus.FieldLogger) *Coordinator {

	ch := make(chan int, 1)
	ch <- 1
	return &Coordinator{
		reManager:        reManager,
		getConfig:        getConfig,
		getExploreResult: getExploreResult,
		getActive:        getActive,
		option:           option,
		log:              log,
		concurrencyLock:  ch,
	}
}

// Run do coordinate periodically until ctx done
func (c *Coordinator) Run(ctx context.Context) error {
	return wait.RunUntil(ctx, c.log, c.option.Period, c.runOnce)
}

// LastGlobalScrapeStatus return the last scraping status of all targets
func (c *Coordinator) LastGlobalScrapeStatus() map[uint64]*target.ScrapeStatus {
	return c.lastGlobalScrapeStatus
}

// runOnce get shards information from shard manager,
// do shard reBalance and change expect shard number
func (c *Coordinator) runOnce() error {
	<- c.concurrencyLock
	defer func() {
		c.concurrencyLock <- 1
		c.log.Debug("finish coordinate.")
	}()

	replicas, err := c.reManager.Replicas()
	if err != nil {
		return errors.Wrapf(err, "get replicas")
	}

	newLastGlobalScrapeStatus := map[uint64]*target.ScrapeStatus{}
	for _, repItem := range replicas {
		shards, err := repItem.Shards()
		if err != nil {
			c.log.Error(err.Error())
			continue
		}

		var (
			active           = c.getActive()
			shardsInfo       = c.getShardInfos(shards)
			changeAbleShards = changeAbleShardsInfo(shardsInfo)
		)

		if int32(len(changeAbleShards)) < c.option.MinShard { // insure that scaling up to min shard
			if err := repItem.ChangeScale(c.option.MinShard); err != nil {
				c.log.Error(err.Error())
				continue
			}
		}

		lastGlobalScrapeStatus := c.globalScrapeStatus(active, shardsInfo)
		c.gcTargets(changeAbleShards, active)
		needSpace := c.alleviateShards(changeAbleShards)
		needSpace += c.assignNoScrapingTargets(shardsInfo, active, lastGlobalScrapeStatus)

		scale := int32(len(shardsInfo))
		if needSpace != 0 {
			c.log.Infof("need space %d", needSpace)
			scale = c.tryScaleUp(shardsInfo, needSpace)
		} else if c.option.MaxIdleTime != 0 {
			scale = c.tryScaleDown(shardsInfo)
		}

		if scale > c.option.MaxShard {
			scale = c.option.MaxShard
		}

		if scale < c.option.MinShard {
			scale = c.option.MinShard
		}

		updateScrapingTargets(shardsInfo, active)
		c.applyShardsInfo(shardsInfo)
		if err := repItem.ChangeScale(scale); err != nil {
			c.log.Error(err.Error())
			continue
		}

		lastGlobalScrapeStatus = c.updateScrapeStatusShards(shardsInfo, lastGlobalScrapeStatus)
		newLastGlobalScrapeStatus = mergeScrapeStatus(newLastGlobalScrapeStatus, lastGlobalScrapeStatus)
	}

	c.lastGlobalScrapeStatus = newLastGlobalScrapeStatus
	return nil
}

// Run do rebalance periodically until ctx done
func (c *Coordinator) RunRebalance(ctx context.Context) error {
	return wait.RunUntil(ctx, c.log, c.option.RebalancePeriod, c.runRebalanceOnce)
}

// runRebalanceOnce will rebalance targets for every job, and then will try to rebalance between shards
func (c *Coordinator) runRebalanceOnce() error {
	<- c.concurrencyLock
	defer func() {
		c.concurrencyLock <- 1
		c.log.Debug("finish rebalance.")
	}()

	replicas, err := c.reManager.Replicas()
	if err != nil {
		return errors.Wrapf(err, "get replicas")
	}

	for _, repItem := range replicas {
		shards, err := repItem.Shards()
		if err != nil {
			c.log.Error(err.Error())
			continue
		}

		var (
			active           = c.getActive()
			shardsInfo       = c.getShardInfos(shards)
			changeAbleShards = changeAbleShardsInfo(shardsInfo)
		)

		c.lastGlobalScrapeStatus = c.globalScrapeStatus(active, shardsInfo)
		// if there many target is unhealth, skip rebalance
		healthRate := getScrapeHealthRate(c.lastGlobalScrapeStatus)
		minHealthRate := 0.95
		if healthRate < minHealthRate {
			c.log.Warnf("scrape status health rate is smaller than %f, cancel to rebalance.", minHealthRate)
			return nil
		}

		c.gcTargets(changeAbleShards, active)
		needSpace := c.assignNoScrapingTargets(shardsInfo, active, c.lastGlobalScrapeStatus)
		if needSpace > 0 {
			c.log.Infof("need space is more then 0, skip rebalance.")
			return nil
		}

		// grouping targets by job
		groupedTargets := getGroupedTargets(active)
		_, _ , shardsState , shardSeries , shardsMap, _ := getGroupedScrapingInfo(shardsInfo, c.lastGlobalScrapeStatus, groupedTargets)
		for k, s := range shardSeries {
			c.log.Debugf("shard: %s, series: %d", k, s)
		}

		for group, targets := range groupedTargets {
			if len(targets) <= 1 {
				c.log.Debugf("only 1 target in group[%s], skip try rebalance", group)
				for h := range targets {
					c.log.Debugf("group [%s] total series: %d", group, c.lastGlobalScrapeStatus[h].Series)
				}
				continue
			}

			bestVector := c.tryRebalanceForOneGroup(group, targets, shardsState, shardsMap)
			for h, v := range bestVector {
				transferTarget(v.from, v.to, h)
			}
		}
		shardsGroupedSeries, shardsGroupedScraping , _ , shardSeries , shardsMap, totalSeries := getGroupedScrapingInfo(shardsInfo, c.lastGlobalScrapeStatus, groupedTargets)

		vectors := c.tryRebalanceBetweenGroups(shardsMap, groupedTargets, shardSeries, shardsGroupedSeries, shardsGroupedScraping, totalSeries)
		for h, v := range vectors {
			transferTarget(v.from, v.to, h)
		}

		updateScrapingTargets(shardsInfo, active)
		c.applyShardsInfo(shardsInfo)

	}

	return nil;

}

func (c *Coordinator) tryRebalanceForOneGroup(
	groupName string,
//shards []*shardInfo,
	targets map[uint64]*discovery.SDTargets,
	shardsState map[string]*simpleShardState,
	shardsMap map[string]*shardInfo,
) map[uint64]*vector {

	shardSeries, avg, currSD := getShardStandardDeviation(shardsState, targets, c.lastGlobalScrapeStatus)
	bestSD := float64(c.option.MaxSeries)
	minShard, maxShard := getMinMaxSeries(shardSeries)
	c.log.Debugf("**** Before rebalance: ****")
	c.log.Debugf("SD for Group :%s, total series: %f, avg: %f, sd: %f", groupName, avg * float64(len(shardSeries)), avg, currSD)
	for n, s := range shardSeries {
		c.log.Debugf("Shard %s has %d series", n, s)
	}

	if !math.IsNaN(currSD) {
		bestSD = currSD
		if avg == 0 || float64(shardSeries[maxShard] - shardSeries[minShard]) < (avg / 4) {
			if currSD < (avg / 4) {
				c.log.Debugf("Group: %s, Avg: %f, SD: %f, balance enough, no need to rebalance", groupName, avg, currSD)
				return nil
			}
		}
	}

	loop := 0
	bestVector := make(map[uint64]*vector)
	targetVector := make(map[uint64]*vector)

	for loop < 10{
		if isAllTargetsDistributed(targets, shardSeries) {
			c.log.Debugf("all targets already assigned to different shards")
			break
		}

		minShard, maxShard := getMinMaxSeries(shardSeries)

		if float64(shardSeries[maxShard] - shardSeries[minShard]) < (avg / 10) {
			// it seems like balance enough, break
			break
		}

		diff := int64(math.Abs(avg - float64(shardSeries[minShard])))
		t := getClosestTarget(shardsState[maxShard], diff, targets)
		c.log.Debugf("closest target: %d, series: %d", t, c.lastGlobalScrapeStatus[t].Series)
		if shardSeries[minShard] + c.lastGlobalScrapeStatus[t].Series <= c.option.MaxSeries {
			// move target from maxShard to minShard
			shardsState[minShard].headSeries = shardsState[minShard].headSeries + c.lastGlobalScrapeStatus[t].Series
			shardsState[maxShard].headSeries = shardsState[maxShard].headSeries - c.lastGlobalScrapeStatus[t].Series
			delete(shardsState[maxShard].scraping, t)
			shardsState[minShard].scraping[t] = c.lastGlobalScrapeStatus[t].Series
			if v, ok := targetVector[t]; ok {
				v.to = shardsMap[minShard]
			} else {
				targetVector[t] = &vector{
					from: shardsMap[maxShard],
					to:   shardsMap[minShard],
				}
			}

		} else {
			break
		}

		c.log.Debugf("minShard series %d, maxShard series %d", shardsState[minShard].headSeries, shardsState[maxShard].headSeries)

		shardSeries, avg, currSD = getShardStandardDeviation(shardsState, targets, c.lastGlobalScrapeStatus)
		if currSD < bestSD {
			bestSD = currSD

			bestVector = make(map[uint64]*vector)
			for k, v := range targetVector {
				bestVector[k] = v
			}
			loop = 0
		} else {
			loop++
		}
	}

	c.log.Debugf("**** After rebalance: ****")
	c.log.Debugf("SD for Group :%s, avg: %f, sd: %f", groupName, avg, currSD)
	for n, s := range shardSeries {
		c.log.Debugf("Shard %s has %d series", n, s)
	}

	return bestVector
}

func (c *Coordinator) tryRebalanceBetweenGroups(
	shardsMap map[string]*shardInfo,
	groupedTargets map[string]map[uint64]*discovery.SDTargets,
	shardSeries map[string]int64,
	shardsGroupedSeries map[string]map[string]int64,
	shardsGroupedScraping map[string]map[string]map[uint64]int64,
	totalSeries int64,
)  map[uint64]*vector {
	targetVector := make(map[uint64]*vector)
	min, max := getMinMaxSeries(shardSeries)
	avg := totalSeries / int64(len(shardsMap))
	if shardSeries[max] - shardSeries[min] < avg / 4 {
		c.log.Infof("balance enough between shards, skip")
		return targetVector
	}

	for i:=0; i <= 10; i++ {
		min, max := getMinMaxSeries(shardSeries)
		diff := shardSeries[max] - shardSeries[min]
		for group, targets := range groupedTargets {
			// only rebalance the shards that have a few targets
			if len(targets) >= len(shardsMap) {
				continue
			}

			// only move the target to the shard that not have the target of this group
			v1, ok1 := shardsGroupedSeries[max][group]
			v2, ok2 := shardsGroupedSeries[min][group]
			if !ok1 || ok2 || v1 <=0 || v2 > 0{
				continue
			}

			diff2 :=  math.Abs(float64((shardSeries[max] - shardsGroupedSeries[max][group]) - (shardSeries[min] + shardsGroupedSeries[max][group])))
			if int64(diff2) < diff {
				i = 0
				for hash := range shardsGroupedScraping[max][group] {
					if v, ok := targetVector[hash]; ok {
						v.to = shardsMap[min]
					} else {
						targetVector[hash] = &vector{
							from: shardsMap[max],
							to:   shardsMap[min],
						}
					}
				}
				shardsGroupedScraping[min][group] = shardsGroupedScraping[max][group]
				delete(shardsGroupedScraping[max], group)

				shardSeries[max] -= shardsGroupedSeries[max][group]
				shardSeries[min] += shardsGroupedSeries[max][group]

				shardsGroupedSeries[min][group] = shardsGroupedSeries[max][group]
				shardsGroupedSeries[max][group] = 0

			}

		}
	}
	return targetVector

}