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
	wr "github.com/mroth/weightedrand"
	"github.com/prometheus/prometheus/scrape"
	"golang.org/x/sync/errgroup"
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

func newShardInfo(sd *shard.Shard) *shardInfo {
	return &shardInfo{
		shard:      sd,
		runtime:    &shard.RuntimeInfo{},
		newTargets: map[string][]*target.Target{},
	}
}

func (s *shardInfo) totalTargetsSeries() int64 {
	ret := int64(0)
	for _, tar := range s.scraping {
		if tar.TargetState != target.StateNormal || tar.Health != scrape.HealthGood || tar.ScrapeTimes < minWaitScrapeTimes {
			continue
		}

		ret += tar.Series
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
			all[i] = c.getOneShardInfo(s)
			return nil
		})
	}
	_ = g.Wait()
	return all
}

func (c *Coordinator) getOneShardInfo(s *shard.Shard) *shardInfo {
	var (
		err error
		si  = newShardInfo(s)
	)

	if !s.Ready {
		c.log.Infof("%s is not ready", s.ID)
		return si
	}

	si.scraping, err = s.TargetStatus()
	if err != nil {
		c.log.Error(err.Error())
		return si
	}

	si.runtime, err = s.RuntimeInfo()
	if err != nil {
		c.log.Error(err.Error())
		return si
	}

	cfg := c.getConfig()
	// try update config to send raw config to
	if si.runtime.ConfigHash != cfg.ConfigHash {
		c.log.Infof("shard %s config need update", si.shard.ID)
		if err := s.UpdateConfig(&shard.UpdateConfigRequest{
			RawContent: string(cfg.RawContent),
		}); err != nil {
			c.log.Error(err.Error())
			return si
		}

		// reload runtime
		si.runtime, err = s.RuntimeInfo()
		if err != nil {
			c.log.Error(err.Error())
			return si
		}
	}

	if si.runtime.ConfigHash != cfg.ConfigHash {
		c.log.Warnf("config of %s is not up to date, expect md5 = %s, shard md5 = %s", si.shard.ID, cfg.ConfigHash, si.runtime.ConfigHash)
		return si
	}

	si.changeAble = true
	return si
}

func (c *Coordinator) applyShardsInfo(shards []*shardInfo) {
	g := errgroup.Group{}
	for _, tmp := range shards {
		s := tmp
		if !s.changeAble {
			c.log.Warnf("shard %s is unHealth, skip apply change", s.shard.ID)
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

func (c *Coordinator) updateScrapeStatusShards(shards []*shardInfo, status map[uint64]*target.ScrapeStatus) map[uint64]*target.ScrapeStatus {
	for _, s := range shards {
		if !s.changeAble {
			continue
		}
		for k := range s.scraping {
			status[k].Shards = append(status[k].Shards, s.shard.ID)
		}
	}
	return status
}

// gcTargets delete targets with following conditions
// 1. not exist in active targets
// 2. is in_transfer state and had been scraped by other shard
// 3. is normal state and had been scraped by other shard with lower head series
func (c *Coordinator) gcTargets(changeAbleShards []*shardInfo, active map[uint64]*discovery.SDTargets) {
	for _, s := range changeAbleShards {
		for h, tar := range s.scraping {
			// target not exist in active targets
			if _, exist := active[h]; !exist {
				delete(s.scraping, h)
				continue
			}

			if tar.ScrapeTimes < minWaitScrapeTimes {
				continue
			}

			for _, other := range changeAbleShards {
				if s == other {
					continue
				}
				st := other.scraping[h]
				if st != nil && st.ScrapeTimes >= minWaitScrapeTimes {
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
func (c *Coordinator) alleviateShards(changeAbleShards []*shardInfo) (needSpace int64) {
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
			if s.runtime.HeadSeries >= seriesWithRate(c.option.MaxSeries, t.maxSeriesRate) {
				c.log.Infof("%s series is %d, over rate %f", s.shard.ID, s.runtime.HeadSeries, t.maxSeriesRate)
				needSpace += c.alleviateShard(s, changeAbleShards, seriesWithRate(c.option.MaxSeries, t.expectSeriesRate))
				break
			}
		}
	}

	return needSpace
}

func (c *Coordinator) alleviateShard(s *shardInfo, changeAbleShards []*shardInfo, expSeries int64) (needSpace int64) {
	total := s.totalTargetsSeries()
	if total <= expSeries {
		return 0
	}

	c.log.Infof("%s need alleviate", s.shard.ID)
	for hash, tar := range s.scraping {
		if total <= expSeries {
			break
		}

		if tar.TargetState != target.StateNormal || tar.Health != scrape.HealthGood || tar.ScrapeTimes < minWaitScrapeTimes {
			continue
		}

		if tar.Series > c.option.MaxSeries {
			c.log.Warnf("too big series [%d] series is [%d], skip alleviate", hash, tar.Series)
			return 0
		}

		// try transfer target to other shard
		for _, os := range changeAbleShards {
			if os == s {
				continue
			}

			if os.runtime.HeadSeries+tar.Series < c.option.MaxSeries {
				c.log.Infof("transfer target from %s to %s series = (%d) ", s.shard.ID, os.shard.ID, tar.Series)
				transferTarget(s, os, hash)
				total -= tar.Series
			}
		}
	}

	if total > expSeries {
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
	globalScrapeStatus map[uint64]*target.ScrapeStatus,
) (needSpace int64) {
	healthShards := changeAbleShardsInfo(shards)
	scraping := map[uint64]bool{}
	for _, s := range shards {
		for hash := range s.scraping {
			scraping[hash] = true
		}
	}

	for hash, tar := range active {
		if scraping[hash] {
			continue
		}

		status := globalScrapeStatus[hash]
		if status == nil || status.Health != scrape.HealthGood {
			continue
		}

		if status.Series > c.option.MaxSeries {
			c.log.Warnf("target too big: %s", tar.ShardTarget.NoParamURL())
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
	cs := make([]wr.Choice, 0)
	for _, s := range shards {
		if s.changeAble && s.runtime.HeadSeries+series < c.option.MaxSeries {
			if c.option.MaxIdleTime != 0 {
				return s
			}
			cs = append(cs, wr.Choice{
				Item:   s,
				Weight: uint(c.option.MaxSeries - s.runtime.HeadSeries),
			})
		}
	}

	if len(cs) == 0 {
		return nil
	}

	cr, _ := wr.NewChooser(cs...)
	return cr.Pick().(*shardInfo)
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
		if s.changeAble && s.runtime.IdleStartAt != nil && time.Now().Sub(*s.runtime.IdleStartAt) > c.option.MaxIdleTime {
			c.log.Infof("%s is remove able", s.shard.ID)
			scale--
		} else {
			break
		}
	}

	// try transfer targets from tail shard to head shards
	for ; i > 0; i-- {
		from := shards[i]
		// skip idle shard
		if from.runtime.IdleStartAt != nil {
			continue
		}

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
			spaces = append(spaces, c.option.MaxSeries-s.runtime.HeadSeries)
		}
	}

l1:
	for _, tar := range src.scraping {
		if tar.TargetState != target.StateNormal || tar.ScrapeTimes < minWaitScrapeTimes {
			return false
		}

		for i := range spaces {
			if spaces[i] > tar.Series {
				spaces[i] -= tar.Series
				continue l1
			}
		}

		return false
	}

	return true
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
func (c *Coordinator) tryScaleUp(shard []*shardInfo, needSpace int64) int32 {
	health := changeAbleShardsInfo(shard)
	exp := int32(len(health))
	exp += int32((needSpace / c.option.MaxSeries) + 1)

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
			old = v
		}
		old.Shards = append(old.Shards, v.Shards...)
	}
	return a
}
