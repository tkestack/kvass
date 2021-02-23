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
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/wait"

	"github.com/sirupsen/logrus"
)

// Coordinator periodically re balance all replicates
type Coordinator struct {
	log                    logrus.FieldLogger
	shardManager           shard.Manager
	maxSeries              int64
	maxShard               int32
	maxIdleTime            time.Duration
	period                 time.Duration
	lastGlobalScrapeStatus map[uint64]*target.ScrapeStatus

	getConfigMd5     func() string
	getExploreResult func(hash uint64) *target.ScrapeStatus
	getActive        func() map[uint64]*discovery.SDTargets
}

// NewCoordinator create a new coordinator service
func NewCoordinator(
	shardManager shard.Manager,
	maxSeries int64,
	maxShard int32,
	maxIdleTime time.Duration,
	period time.Duration,
	getConfigMd5 func() string,
	getExploreResult func(hash uint64) *target.ScrapeStatus,
	getActive func() map[uint64]*discovery.SDTargets,
	log logrus.FieldLogger) *Coordinator {
	return &Coordinator{
		shardManager:     shardManager,
		getConfigMd5:     getConfigMd5,
		getExploreResult: getExploreResult,
		getActive:        getActive,
		maxShard:         maxShard,
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
	shards, err := c.shardManager.Shards()
	if err != nil {
		return errors.Wrapf(err, "Shards")
	}

	var (
		active           = c.getActive()
		shardsInfo       = getShardInfos(shards, c.getConfigMd5(), c.log)
		changeAbleShards = changeAbleShardsInfo(shardsInfo)
	)

	c.lastGlobalScrapeStatus = globalScrapeStatus(active, shardsInfo, c.getExploreResult)
	gcTargets(changeAbleShards, active, c.log)
	needSpace := alleviateShards(changeAbleShards, c.maxSeries, c.log)
	needSpace += assignNoScrapingTargets(shardsInfo, active, c.maxSeries, c.lastGlobalScrapeStatus)

	scale := int32(len(shardsInfo))
	if needSpace != 0 {
		c.log.Infof("need space %d", needSpace)
		scale = tryScaleUp(shardsInfo, needSpace, c.maxSeries, c.maxShard)
	} else if c.maxIdleTime != 0 {
		scale = tryScaleDown(shardsInfo, c.maxSeries, c.maxIdleTime, c.log)
	}

	updateScrapingTargets(shardsInfo, active)
	applyShardsInfo(shardsInfo, c.log)
	return c.shardManager.ChangeScale(scale)
}
