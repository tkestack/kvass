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
	log logrus.FieldLogger) *Coordinator {
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

		newLastGlobalScrapeStatus = mergeScrapeStatus(newLastGlobalScrapeStatus, lastGlobalScrapeStatus)
	}

	c.lastGlobalScrapeStatus = newLastGlobalScrapeStatus
	return nil
}
