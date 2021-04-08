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
	"fmt"
	"github.com/prometheus/prometheus/scrape"
	"golang.org/x/sync/errgroup"
	"math/rand"
	"time"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
)

const (
	minWaitScrapeTimes = 3
)

type shardInfo struct {
	changeAble bool
	shard      *shard.Shard
	runtime    *shard.RuntimeInfo
	scraping   map[uint64]*target.ScrapeStatus
	newTargets map[string][]*target.Target
}

func newShardInfo(shard *shard.Shard) *shardInfo {
	return &shardInfo{
		changeAble: true,
		shard:      shard,
		newTargets: map[string][]*target.Target{},
	}
}

func (s *shardInfo) totalTargetsSeries() int64 {
	ret := int64(0)
	for _, s := range s.scraping {
		if s.TargetState != target.StateNormal {
			continue
		}
		ret += s.Series
	}
	return ret
}

func changeAbleShardsInfo(shards []*shardInfo) []*shardInfo {
	ret := make([]*shardInfo, 0)
	for _, s := range shards {
		if s.changeAble {
			ret = append(ret, s)
		}
	}
	return ret
}

func updateScrapingTargets(shards []*shardInfo, active map[uint64]*discovery.SDTargets) {
	for _, s := range shards {
		s.newTargets = map[string][]*target.Target{}
		for hash, c := range s.scraping {
			tar := active[hash]
			if tar == nil {
				continue
			}

			t := *tar.ShardTarget
			t.TargetState = c.TargetState
			t.Series = c.Series
			s.newTargets[tar.Job] = append(s.newTargets[tar.Job], &t)
		}
	}
}

func (c *Coordinator) getShardInfos(shards []*shard.Shard) []*shardInfo {
	all := make([]*shardInfo, len(shards))
	g := errgroup.Group{}
	for index, tmp := range shards {
		s := tmp
		i := index
		g.Go(func() (err error) {
			si := newShardInfo(s)
			all[i] = si
			if !s.Ready {
				c.log.Infof("%s is not ready", s.ID)
				si.changeAble = false
				return nil
			}
			si.scraping, err = s.TargetStatus()
			if err != nil {
				c.log.Error(err.Error())
				si.changeAble = false
			}

			si.runtime, err = s.RuntimeInfo()
			if err != nil {
				c.log.Error(err.Error())
				si.changeAble = false
			} else {
				if si.runtime.ConfigHash != c.getConfigMd5() {
					c.log.Warnf("config of %s is not up to date, expect md5 = %s, shard md5 = %s", si.shard.ID, c.getConfigMd5(), si.runtime.ConfigHash)
					si.changeAble = false
				}
			}

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
		if !s.changeAble {
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

// gcTargets delete targets with following conditions
// 1. not exist in active targets
// 2. is in_transfer state and had been scraped by other shard
// 3. is normal state and had been scraped by other shard with lower head series
func (c *Coordinator) gcTargets(changeAbleShards []*shardInfo, active , globalTargets map[uint64]*discovery.SDTargets) {
	for _, s := range changeAbleShards {
		for h, tar := range s.scraping {
			// target not exist in active targets
			if _, exist := active[h]; !exist {
				delete(s.scraping, h)
				continue
			} else {
				if _, exist := globalTargets[h];exist {
					continue
				}
			}

			if tar.ScrapeTimes < minWaitScrapeTimes {
				continue
			}

			for _, other := range changeAbleShards {
				if s == other {
					continue
				}
				st := other.scraping[h]
				if st != nil && st.Health == scrape.HealthGood && st.ScrapeTimes >= minWaitScrapeTimes {
					// is in_transfer state and had been scraped by other shard
					if (tar.TargetState == target.StateInTransfer && st.TargetState == target.StateNormal) ||
						// is in normal state and had been scraped by other shard with lower head series
						(tar.TargetState == target.StateNormal &&
							st.TargetState == target.StateNormal &&
							other.runtime.HeadSeries < s.runtime.HeadSeries) {
						delete(s.scraping, h)
						break
					}
				}
			}
		}
	}
}

// alleviateShards try remove some targets from shards to alleviate shard burden
// make expect series of targets less than maxSeries * 0.5 if current head series > maxSeries 1.4
// make expect series of targets less than maxSeries * 0.2 if current head series > maxSeries 1.6
// remove all targets if current head series > maxSeries 1.8
func (c *Coordinator) alleviateShards(changeAbleShards []*shardInfo, globalTargets map[uint64]*discovery.SDTargets) (needSpace int64) {
	var threshold = []struct {
		maxSeriesRate    float64
		expectSeriesRate float64
	}{
		{
			maxSeriesRate:    1.8,
			expectSeriesRate: 0,
		},
		{
			maxSeriesRate:    1.6,
			expectSeriesRate: 0.2,
		},
		{
			maxSeriesRate:    1.4,
			expectSeriesRate: 0.5,
		},
		{
			maxSeriesRate:    1.1,
			expectSeriesRate: 1,
		},
	}

	for _, s := range changeAbleShards {
		for _, t := range threshold {
			if s.runtime.HeadSeries >= seriesWithRate(c.maxSeries, t.maxSeriesRate) {
				c.log.Infof("%s need alleviate", s.shard.ID)
				needSpace += c.alleviateShard(s, changeAbleShards, seriesWithRate(c.maxSeries, t.expectSeriesRate), globalTargets)
				break
			}
		}
	}

	return needSpace
}

func (c *Coordinator) alleviateShard(s *shardInfo, changeAbleShards []*shardInfo, expSeries int64, globalTargets map[uint64]*discovery.SDTargets) (needSpace int64) {
	total := s.totalTargetsSeries()
	for hash, tar := range s.scraping {
		if total < expSeries {
			break
		}

		if _, isGlobal := globalTargets[hash]; isGlobal {
			continue
		}

		if tar.TargetState != target.StateNormal || tar.Health != scrape.HealthGood || tar.ScrapeTimes < minWaitScrapeTimes {
			continue
		}

		// try transfer target to other shard
		for _, os := range changeAbleShards {
			if os == s {
				continue
			}

			if os.runtime.HeadSeries+tar.Series < c.maxSeries {
				c.log.Infof("transfer target from %s to %s series = (%d) ", s.shard.ID, os.shard.ID, tar.Series)
				transferTarget(s, os, hash)
				total -= tar.Series
			}
		}
	}

	if total >= expSeries {
		return total - expSeries
	}
	return 0
}

func transferTarget(from, to *shardInfo, hash uint64) {
	tar := from.scraping[hash]
	to.runtime.HeadSeries += tar.Series
	newTar := *tar
	tar.TargetState = target.StateInTransfer
	to.scraping[hash] = &newTar
}

func seriesWithRate(series int64, rate float64) int64 {
	return int64(float64(series) * rate)
}

// assignNoScrapingTargets assign targets that no shard is scraping
func (c *Coordinator) assignNoScrapingTargets(
	shards []*shardInfo,
	active map[uint64]*discovery.SDTargets,
	globalTargets map[uint64]*discovery.SDTargets,
	globalScrapeStatus map[uint64]*target.ScrapeStatus,
) (needSpace int64) {
	healthShards := changeAbleShardsInfo(shards)
	scraping := map[uint64]bool{}
	for _, s := range shards {
		// Check and assign global targets for every shard.
		for h, _ := range globalTargets {
			if _, exist := s.scraping[h]; s.scraping != nil && !exist {
				s.scraping[h] = globalScrapeStatus[h]
				s.runtime.HeadSeries += globalScrapeStatus[h].Series
			}
		}

		for hash := range s.scraping {
			scraping[hash] = true
		}
	}

	for hash := range active {
		//isGlobal := false
		//if _, isGlobal = globalTargets[hash]; scraping[hash] && !isGlobal{
		//	continue
		//}

		if scraping[hash]{
			continue
		}

		status := globalScrapeStatus[hash]
		if status == nil || status.Health != scrape.HealthGood {
			continue
		}

		sd := c.getFreeShard(healthShards, status.Series)
		if sd != nil {
			sd.runtime.HeadSeries += status.Series
			sd.scraping[hash] = status
		} else {
			needSpace += status.Series
		}
	}
	return needSpace
}

func (c *Coordinator) getFreeShard(shards []*shardInfo, series int64) *shardInfo {
	cond := make([]*shardInfo, 0)
	isRand := c.maxIdleTime == 0
	for _, s := range shards {
		if s.changeAble && s.runtime.HeadSeries+series < c.maxSeries {
			if !isRand {
				return s
			}
			cond = append(cond, s)
		}
	}

	if len(cond) == 0 {
		return nil
	}

	return cond[rand.Uint64()%uint64(len(cond))]
}

func (c *Coordinator) globalScrapeStatus(
	active map[uint64]*discovery.SDTargets,
	shards []*shardInfo,
) map[uint64]*target.ScrapeStatus {
	ret := map[uint64]*target.ScrapeStatus{}
l1:
	for h := range active {
		for _, s := range shards {
			if s.scraping[h] != nil && s.scraping[h].Health != scrape.HealthUnknown {
				ret[h] = s.scraping[h]
				continue l1
			}
		}

		// try found status from exploring
		status := c.getExploreResult(h)
		if status != nil {
			ret[h] = status
		} else {
			ret[h] = target.NewScrapeStatus(0)
		}
	}

	return ret
}

// tryScaleDown try transfer targets in tail shard to front and make it idle
// idle shard may be delete
func (c *Coordinator) tryScaleDown(shards []*shardInfo) int32 {
	var (
		scale = int32(len(shards))
		i     = len(shards) - 1
	)

	// check for scale able shard
	for ; i >= 0; i-- {
		s := shards[i]
		if s.changeAble && s.runtime.IdleStartAt != nil && time.Now().Sub(*s.runtime.IdleStartAt) > c.maxIdleTime {
			c.log.Infof("%s is remove able", s.shard.ID)
			scale--
		} else {
			break
		}
	}

	// try transfer targets from tail shard to head shards
	for ; i > 0; i-- {
		from := shards[i]
		if !c.shardCanBeIdle(from, shards[0:i]) {
			return scale
		}

		c.log.Infof("try mark transfer all targets from %s", from.shard.ID)
		if !c.shardBecomeIdle(from, shards[0:i]) {
			return scale
		}
	}

	return scale
}

// shardCanBeIdle return true if all targets of src can be transfer to other
func (c *Coordinator) shardCanBeIdle(src *shardInfo, shards []*shardInfo) bool {
	if !src.changeAble {
		return false
	}

	spaces := make([]int64, 0)
	for _, s := range shards {
		if s != src && s.changeAble {
			spaces = append(spaces, c.maxSeries-s.runtime.HeadSeries)
		}
	}

	total := 0
l1:
	for _, tar := range src.scraping {
		if tar.TargetState != target.StateNormal || tar.ScrapeTimes < minWaitScrapeTimes {
			return false
		}
		total++

		for i := range spaces {
			if spaces[i] > tar.Series {
				spaces[i] -= tar.Series
				continue l1
			}
		}

		return false
	}

	return total != 0
}

func (c *Coordinator) shardBecomeIdle(src *shardInfo, shards []*shardInfo) bool {
	for hash, tar := range src.scraping {
		if tar.TargetState != target.StateNormal || tar.ScrapeTimes < minWaitScrapeTimes {
			continue
		}

		to := c.getFreeShard(shards, tar.Series)
		// no free space to receive target
		if to == nil || to == src {
			return false
		}
		c.log.Infof("transfer target from %s to %s series = (%d) ", src.shard.ID, to.shard.ID, tar.Series)
		transferTarget(src, to, hash)
	}

	return true
}

// tryScaleUp calculate the expect scale according to 'needSpace'
func (c *Coordinator) tryScaleUp(shard []*shardInfo, globalSeries, needSpace int64) int32 {
	health := changeAbleShardsInfo(shard)
	free := c.maxSeries - globalSeries
	c.log.Info(fmt.Sprintf("After global target assigned, free space: %d", free))
	exp := int32(len(health))
	exp += int32((needSpace / free) + 1)

	if exp < int32(len(shard)) {
		exp = int32(len(shard))
	}
	return exp
}

func mergeScrapeStatus(a, b map[uint64]*target.ScrapeStatus) map[uint64]*target.ScrapeStatus {
	for k, v := range b {
		old := a[k]
		if old == nil ||
			(old.Health != scrape.HealthGood && v.Health == scrape.HealthGood) ||
			(v.Health == scrape.HealthGood && v.Series > old.Series) {
			a[k] = v
		}
	}
	return a
}
