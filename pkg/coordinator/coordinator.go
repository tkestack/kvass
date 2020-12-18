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
	"github.com/prometheus/prometheus/scrape"
	"golang.org/x/sync/errgroup"
	"time"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/wait"

	"github.com/sirupsen/logrus"
)

// Coordinator periodically re balance all replicates
type Coordinator struct {
	log          logrus.FieldLogger
	shardManager shard.Manager
	maxSeries    int64
	maxShard     int32
	period       time.Duration

	getExploreResult func(job string, hash uint64) *target.ScrapeStatus
	getActive        func() map[string][]*discovery.SDTargets
}

// NewCoordinator create a new coordinator service
func NewCoordinator(
	shardManager shard.Manager,
	maxSeries int64,
	maxShard int32,
	period time.Duration,
	getExploreResult func(job string, hash uint64) *target.ScrapeStatus,
	getActive func() map[string][]*discovery.SDTargets,
	log logrus.FieldLogger) *Coordinator {
	return &Coordinator{
		shardManager:     shardManager,
		getExploreResult: getExploreResult,
		getActive:        getActive,
		maxShard:         maxShard,
		maxSeries:        maxSeries,
		log:              log,
		period:           period,
	}
}

// Run do coordinate periodically until ctx done
func (c *Coordinator) Run(ctx context.Context) error {
	return wait.RunUntil(ctx, c.log, c.period, c.RunOnce)
}

type shardInfo struct {
	isHealth   bool
	shard      *shard.Group
	runtime    *shard.RuntimeInfo
	scraping   map[uint64]bool
	newTargets map[string][]*target.Target
}

// RunOnce get shards information from shard manager,
// do shard reBalance and change expect shard number
func (c *Coordinator) RunOnce() error {
	shards, err := c.shardManager.Shards()
	if err != nil {
		return errors.Wrapf(err, "Shards")
	}

	shardsInfo := c.getShardsInfo(shards)
	scraping := c.globalScraping(shardsInfo)
	exp, err := c.reBalance(shardsInfo, scraping)
	if err != nil {
		return errors.Wrapf(err, "reBalance")
	}

	return c.shardManager.ChangeScale(exp)
}

func (c *Coordinator) reBalance(
	shardsInfo []*shardInfo,
	scraping map[uint64]*shardInfo,
) (int32, error) {
	needSpace := int64(0)
	for job, ts := range c.getActive() {
		for _, target := range ts {
			t := target.ShardTarget
			sd := scraping[t.Hash]
			// target is scraping by this shard group
			if sd != nil {
				sd.newTargets[job] = append(sd.newTargets[job], t)
				continue
			}

			// if no shard scraping this target, we try assign it
			exp := c.getExploreResult(job, t.Hash)
			if exp == nil {
				continue
			}

			// this target is explored, assign it
			if exp.Health == scrape.HealthGood {
				found := false
				if exp.Series > c.maxSeries {
					c.log.Errorf("target too big %s (%d)", t.NoParamURL(), exp.Series)
					continue
				}
				for _, s := range shardsInfo {
					if !s.isHealth {
						continue
					}

					if s.runtime.HeadSeries+exp.Series < c.maxSeries {
						s.runtime.HeadSeries += exp.Series
						t.Series = exp.Series
						s.newTargets[job] = append(s.newTargets[job], t)
						found = true
						break
					}
				}

				if !found {
					needSpace += exp.Series
				}
			}
		}
	}
	c.applyShardsInfo(shardsInfo)

	exp := int32(0)
	for _, s := range shardsInfo {
		if s.isHealth {
			exp++
		}
	}

	if needSpace > 0 {
		exp += int32(needSpace/c.maxSeries + 1)
		c.log.Infof("need space %d need exp = %d", needSpace, int32(needSpace/c.maxSeries+1))
	}

	if exp > c.maxShard {
		c.log.Warnf("expect scale is %d but the max is %d", exp, c.maxShard)
		exp = c.maxShard
	}

	return exp, nil
}

func (c *Coordinator) globalScraping(info []*shardInfo) map[uint64]*shardInfo {
	ret := map[uint64]*shardInfo{}
	for _, s := range info {
		for k := range s.scraping {
			if ret[k] != nil {
				c.log.Warnf("target (%d) is already scraping by %s when %s mark global scraping",
					k, ret[k].shard.ID, s.shard.ID,
				)
				continue
			}
			ret[k] = s
		}
	}

	return ret
}

func (c *Coordinator) getShardsInfo(shards []*shard.Group) []*shardInfo {
	all := make([]*shardInfo, len(shards))
	g := errgroup.Group{}
	for index, tmp := range shards {
		s := tmp
		i := index
		g.Go(func() (err error) {
			si := &shardInfo{
				isHealth:   true,
				shard:      s,
				newTargets: map[string][]*target.Target{},
			}

			si.scraping, err = s.TargetsScraping()
			if err != nil {
				c.log.Error(err.Error())
				si.isHealth = false
				si.scraping = map[uint64]bool{}
			}

			si.runtime, err = s.RuntimeInfo()
			if err != nil {
				c.log.Error(err.Error())
				si.isHealth = false
			}

			all[i] = si
			return nil
		})
	}
	_ = g.Wait()
	return all
}

func (c *Coordinator) applyShardsInfo(shards []*shardInfo) {
	g := errgroup.Group{}
	for _, tmp := range shards {
		s := tmp
		if !s.isHealth {
			c.log.Warnf("shard group %s is unHealth, skip apply change", s.shard.ID)
			continue
		}

		g.Go(func() (err error) {
			if err := s.shard.UpdateTarget(&shard.UpdateTargetsRequest{Targets: s.newTargets}); err != nil {
				c.log.Error(err.Error())
				return err
			}
			return nil
		})
	}
	_ = g.Wait()
}
