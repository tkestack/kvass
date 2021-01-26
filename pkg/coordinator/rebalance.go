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
	"github.com/prometheus/prometheus/scrape"
	"github.com/sirupsen/logrus"
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
	shard      *shard.Group
	runtime    *shard.RuntimeInfo
	scraping   map[uint64]*target.ScrapeStatus
	newTargets map[string][]*target.Target
}

func newShardInfo(shard *shard.Group) *shardInfo {
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

func getShardInfos(shards []*shard.Group, cfgMd5 string, lg logrus.FieldLogger) []*shardInfo {
	all := make([]*shardInfo, len(shards))
	g := errgroup.Group{}
	for index, tmp := range shards {
		s := tmp
		i := index
		g.Go(func() (err error) {
			si := newShardInfo(s)
			si.scraping, err = s.TargetStatus()
			if err != nil {
				lg.Error(err.Error())
				si.changeAble = false
			}

			si.runtime, err = s.RuntimeInfo()
			if err != nil {
				lg.Error(err.Error())
				si.changeAble = false
			} else {
				if si.runtime.ConfigMD5 != cfgMd5 {
					lg.Warnf("config of %s is not up to date", si.shard.ID)
					si.changeAble = false
				}
			}

			all[i] = si
			return nil
		})
	}
	_ = g.Wait()
	return all
}

func applyShardsInfo(shards []*shardInfo, log logrus.FieldLogger) {
	g := errgroup.Group{}
	for _, tmp := range shards {
		s := tmp
		if !s.changeAble {
			log.Warnf("shard group %s is unHealth, skip apply change", s.shard.ID)
			continue
		}

		g.Go(func() (err error) {
			if err := s.shard.UpdateTarget(&shard.UpdateTargetsRequest{Targets: s.newTargets}); err != nil {
				log.Error(err.Error())
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
func gcTargets(changeAbleShards []*shardInfo, active map[uint64]*discovery.SDTargets, log logrus.FieldLogger) {
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
// make expect series of targets less than maxSeries * 0.6 if current head series > maxSeries 1.2
// make expect series of targets less than maxSeries * 0.3 if current head series > maxSeries 1.5
// remove all targets if current head series > maxSeries 1.8
func alleviateShards(changeAbleShards []*shardInfo, maxSeries int64, log logrus.FieldLogger) (needSpace int64) {
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
			if s.runtime.HeadSeries >= seriesWithRate(maxSeries, t.maxSeriesRate) {
				needSpace += alleviateShard(s, changeAbleShards, maxSeries, seriesWithRate(maxSeries, t.expectSeriesRate), log)
				break
			}
		}
	}

	return needSpace
}

func alleviateShard(s *shardInfo, changeAbleShards []*shardInfo, maxSeries int64, expSeries int64, log logrus.FieldLogger) (needSpace int64) {
	total := s.totalTargetsSeries()
	for hash, tar := range s.scraping {
		if total < expSeries {
			break
		}

		if tar.TargetState != target.StateNormal || tar.Health != scrape.HealthGood || tar.ScrapeTimes < minWaitScrapeTimes {
			continue
		}

		// try transfer target to other shard
		for _, os := range changeAbleShards {
			if os == s {
				continue
			}

			if os.runtime.HeadSeries+tar.Series < maxSeries {
				log.Infof("transfer target from %s to %s series = (%d) ", s.shard.ID, os.shard.ID, tar.Series)
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
func assignNoScrapingTargets(
	shards []*shardInfo,
	active map[uint64]*discovery.SDTargets,
	maxSeries int64,
	globalScrapeStatus map[uint64]*target.ScrapeStatus,
) (needSpace int64) {
	healthShards := changeAbleShardsInfo(shards)
	scraping := map[uint64]bool{}
	for _, s := range shards {
		for hash := range s.scraping {
			scraping[hash] = true
		}
	}

	for hash := range active {
		if scraping[hash] {
			continue
		}

		status := globalScrapeStatus[hash]
		if status == nil || status.Health != scrape.HealthGood {
			continue
		}

		sd := getFreeShard(healthShards, status.Series, maxSeries)
		if sd != nil {
			sd.runtime.HeadSeries += status.Series
			sd.scraping[hash] = status
		} else {
			needSpace += status.Series
		}
	}
	return needSpace
}

func getFreeShard(shards []*shardInfo, series int64, maxSeries int64) *shardInfo {
	for _, s := range shards {
		if s.changeAble && s.runtime.HeadSeries+series < maxSeries {
			return s
		}
	}
	return nil
}

func globalScrapeStatus(
	active map[uint64]*discovery.SDTargets,
	shards []*shardInfo,
	getExploreStatus func(uint64) *target.ScrapeStatus,
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
		status := getExploreStatus(h)
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
func tryScaleDown(shards []*shardInfo, maxSeries int64, maxIdle time.Duration, log logrus.FieldLogger) int32 {
	var (
		scale = int32(len(shards))
		i     = len(shards) - 1
	)

	// check for scale able shard
	for ; i >= 0; i-- {
		s := shards[i]
		if s.changeAble && s.runtime.IdleStartAt != nil && time.Now().Sub(*s.runtime.IdleStartAt) > maxIdle {
			log.Infof("%s is remove able", s.shard.ID)
			scale--
		} else {
			break
		}
	}

	// try transfer targets from tail shard to head shards
	for ; i > 0; i-- {
		from := shards[i]
		if !shardCanBeIdle(from, shards[0:i], maxSeries) {
			return scale
		}

		log.Infof("try mark transfer all targets from %s", from.shard.ID)
		if !shardBecomeIdle(from, shards[0:i], maxSeries, log) {
			return scale
		}
	}

	return scale
}

// shardCanBeIdle return true if all targets of src can be transfer to other
func shardCanBeIdle(src *shardInfo, shards []*shardInfo, maxSeries int64) bool {
	if !src.changeAble {
		return false
	}

	spaces := make([]int64, 0)
	for _, s := range shards {
		if s != src && s.changeAble {
			spaces = append(spaces, maxSeries-s.runtime.HeadSeries)
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

func shardBecomeIdle(src *shardInfo, shards []*shardInfo, maxSeries int64, log logrus.FieldLogger) bool {
	for hash, tar := range src.scraping {
		if tar.TargetState != target.StateNormal || tar.ScrapeTimes < minWaitScrapeTimes {
			continue
		}

		to := getFreeShard(shards, tar.Series, maxSeries)
		// no free space to receive target
		if to == nil || to == src {
			return false
		}
		log.Infof("transfer target from %s to %s series = (%d) ", src.shard.ID, to.shard.ID, tar.Series)
		transferTarget(src, to, hash)
	}

	return true
}

// tryScaleUp calculate the expect scale according to 'needSpace'
func tryScaleUp(shard []*shardInfo, needSpace int64, maxSeries int64, maxShard int32) int32 {
	health := changeAbleShardsInfo(shard)
	exp := int32(len(health))
	exp += int32((needSpace / maxSeries) + 1)

	if exp < int32(len(shard)) {
		exp = int32(len(shard))
	}

	if exp > maxShard {
		exp = maxShard
	}
	return exp
}
