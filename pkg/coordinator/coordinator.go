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
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/wait"
)

var (
	coordinatorFailed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kvass_coordinator_failed_total",
	}, []string{})
	assignNoScrapingTargetsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kvass_coordinator_assign_targets_total",
	}, []string{})
	alleviateShardsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kvass_coordinator_alleviate_shards_total",
	}, []string{})
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
	// DisableAlleviate disable shard alleviation when shard is overload
	DisableAlleviate bool
}

// Coordinator periodically re balance all replicates
type Coordinator struct {
	log              logrus.FieldLogger
	reManager        shard.ReplicasManager
	option           *Option
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
	promRegisterer prometheus.Registerer,
	log logrus.FieldLogger,
) *Coordinator {
	_ = promRegisterer.Register(coordinatorFailed)
	_ = promRegisterer.Register(assignNoScrapingTargetsTotal)
	_ = promRegisterer.Register(alleviateShardsTotal)
	return &Coordinator{
		reManager:        reManager,
		getConfig:        getConfig,
		getExploreResult: getExploreResult,
		getActive:        getActive,
		option:           option,
		log:              log,
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

// LastScrapeStatistics collect targets scrape sample statistic from all shards
func (c *Coordinator) LastScrapeStatistics(jobName string, withMetricsDetail bool) (map[string]*scrape.StatisticsSeriesResult, error) {
	rep, err := c.reManager.Replicas()
	if err != nil {
		return nil, err
	}

	ret := map[string]*scrape.StatisticsSeriesResult{}
	for _, m := range rep {
		w := errgroup.Group{}
		rp := map[string]*scrape.StatisticsSeriesResult{}
		lk := sync.Mutex{}
		shards, err := m.Shards()
		if err != nil {
			c.log.Errorf(err.Error())
			continue
		}

		for _, tmp := range shards {
			s := tmp
			if !s.Ready {
				continue
			}

			w.Go(func() error {
				sp, err := s.Samples(jobName, withMetricsDetail)
				if err != nil {
					return err
				}
				lk.Lock()
				defer lk.Unlock()

				for job, result := range sp {
					item := rp[job]
					if item == nil {
						item = scrape.NewStatisticsSeriesResult()
						rp[job] = item
					}

					item.ScrapedTotal += result.ScrapedTotal
					for k, m := range result.MetricsTotal {
						mi := item.MetricsTotal[k]
						if mi == nil {
							mi = &scrape.MetricSamplesInfo{}
							item.MetricsTotal[k] = mi
						}
						mi.Total += m.Total
						mi.Scraped += m.Scraped
					}
				}
				return nil
			})
		}
		_ = w.Wait()

		// merget all replicas
		for k, v := range rp {
			if ret[k] == nil {
				ret[k] = v
			}
		}
	}
	return ret, nil
}

// runOnce get shards information from shard manager,
// do shard reBalance and change expect shard number
func (c *Coordinator) runOnce() (err error) {
	defer func() {
		if err != nil {
			coordinatorFailed.WithLabelValues().Inc()
		}
	}()

	replicas, err := c.reManager.Replicas()
	if err != nil {
		return errors.Wrap(err, "get replicas")
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
