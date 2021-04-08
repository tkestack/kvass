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
	"fmt"
	"github.com/pkg/errors"
	"strings"
	"time"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/wait"

	"github.com/sirupsen/logrus"
)

// Coordinator periodically re balance all replicates
type Coordinator struct {
	log                    logrus.FieldLogger
	reManager              shard.ReplicasManager
	maxSeries              int64
	maxShard               int32
	minShard               int32
	maxIdleTime            time.Duration
	period                 time.Duration
	lastGlobalScrapeStatus map[uint64]*target.ScrapeStatus
	getConfigMd5           func() string
	getExploreResult       func(hash uint64) *target.ScrapeStatus
	getActive              func() map[uint64]*discovery.SDTargets
}

// NewCoordinator create a new coordinator service
func NewCoordinator(
	reManager shard.ReplicasManager,
	maxSeries int64,
	maxShard int32,
	minShard int32,
	maxIdleTime time.Duration,
	period time.Duration,
	getConfigMd5 func() string,
	getExploreResult func(hash uint64) *target.ScrapeStatus,
	getActive func() map[uint64]*discovery.SDTargets,
	log logrus.FieldLogger) *Coordinator {
	return &Coordinator{
		reManager:        reManager,
		getConfigMd5:     getConfigMd5,
		getExploreResult: getExploreResult,
		getActive:        getActive,
		maxShard:         maxShard,
		minShard:         minShard,
		maxSeries:        maxSeries,
		maxIdleTime:      maxIdleTime,
		log:              log,
		period:           period,
	}
}

// Run do coordinate periodically until ctx done
func (c *Coordinator) Run(ctx context.Context) error {
	return wait.RunUntil(ctx, c.log, c.period, c.runOnce)
}

// LastGlobalScrapeStatus return the last scraping status of last coordinate
func (c *Coordinator) LastGlobalScrapeStatus() map[uint64]*target.ScrapeStatus {
	return c.lastGlobalScrapeStatus
}

// runOnce get shards information from shard manager,
// do shard reBalance and change expect shard number
func (c *Coordinator) runOnce() error {
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
			// global targets are those jobs that with "kvass_global_" as its name prefix
			globalTargets 	 = getGlobalTarget(active)
		)

		if int32(len(changeAbleShards)) < c.minShard { // insure that scaling up to min shard
			if err := repItem.ChangeScale(c.minShard); err != nil {
				c.log.Error(err.Error())
				continue
			}
		}

		lastGlobalScrapeStatus := c.globalScrapeStatus(active, shardsInfo)
		c.gcTargets(changeAbleShards, active, globalTargets)
		needSpace := c.alleviateShards(changeAbleShards, globalTargets)
		// TODO: assign global target
		needSpace += c.assignNoScrapingTargets(shardsInfo, active, globalTargets, lastGlobalScrapeStatus)

		scale := int32(len(shardsInfo))
		if needSpace != 0 {
			c.log.Infof("need space %d", needSpace)
			globalSeries := getSeriesTotal(globalTargets, lastGlobalScrapeStatus)
			c.log.Info(fmt.Sprintf("Global series: %d", globalSeries))
			if c.maxSeries - globalSeries <= 0 {
				c.log.Error("There is no enough to assign other targets after global targets has been assigned.")
				return fmt.Errorf("There is no enough to assign other targets after global targets has been assigned")
			}
			scale = c.tryScaleUp(shardsInfo, globalSeries, needSpace)
		} else if c.maxIdleTime != 0 {
			// TODO: if there is only global target in the shard, need to scale down
			scale = c.tryScaleDown(shardsInfo)
		}

		if scale > c.maxShard {
			scale = c.maxShard
		}

		if scale < c.minShard {
			scale = c.minShard
		}

		updateScrapingTargets(shardsInfo, active)
		c.applyShardsInfo(shardsInfo)
		if err := repItem.ChangeScale(scale); err != nil {
			c.log.Error(err.Error())
			continue
		}

		newLastGlobalScrapeStatus = mergeScrapeStatus(newLastGlobalScrapeStatus, lastGlobalScrapeStatus)
	}

	c.lastGlobalScrapeStatus = newLastGlobalScrapeStatus
	return nil
}

func getSeriesTotal(targets map[uint64]*discovery.SDTargets, globalScrapeStatus map[uint64]*target.ScrapeStatus,) int64 {
	var total int64 = 0
	for h, _ := range targets {
		total += globalScrapeStatus[h].Series
	}
	return total
}

func getGlobalTarget(active map[uint64]*discovery.SDTargets) map[uint64]*discovery.SDTargets {
	globalTargets := make(map[uint64]*discovery.SDTargets)
	for k, v := range active {
		if strings.HasPrefix(v.Job, "kvass_global_") {
			globalTargets[k] = v
		}
	}
	return globalTargets
}