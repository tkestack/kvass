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
	"tkestack.io/kvass/pkg/shard"

	"github.com/sirupsen/logrus"
)

// TargetManager manager targets set and auto exploring unknown targets
type TargetManager interface {
	Update(all []*shard.Target)
	Get(hash string) *Target
}

// DefReBalancer is the default implementation of reBalance algorithm
type DefReBalancer struct {
	lg          logrus.FieldLogger
	insManager  ShardsManager
	tManager    TargetManager
	maxSeries   int64
	maxShard    int32
	maxInstance int
}

// NewDefBalancer create a new DefReBalancer
func NewDefBalancer(
	maxSeries int64,
	maxShard int32,
	tManager TargetManager,
	lg logrus.FieldLogger,
) *DefReBalancer {
	return &DefReBalancer{
		lg:        lg,
		tManager:  tManager,
		maxSeries: maxSeries,
		maxShard:  maxShard,
	}
}

// ReBalance reBalance runtimeInfo of all shard and return the expect instance number of shards
func (c *DefReBalancer) ReBalance(rt []*shard.RuntimeInfo) int32 {
	targets, sli := GlobalTargets(rt)
	c.tManager.Update(sli)
	needSpace := int64(0)
	unknowns := targets[shard.IDUnknown]
l1:
	for _, t := range unknowns {
		hash := t.Hash()
		tCache := c.tManager.Get(hash)
		if tCache != nil && tCache.Samples >= 1 {
			for _, r := range rt {
				if r.HeadSeries+tCache.Samples < c.maxSeries {
					r.HeadSeries += tCache.Samples
					delete(unknowns, hash)
					t.Samples = tCache.Samples
					targets[r.ID][hash] = t
					continue l1
				}
			}
			needSpace += tCache.Samples
		}
	}

	newTargets := map[string][]*shard.Target{}
	for id, ts := range targets {
		for _, t := range ts {
			newTargets[id] = append(newTargets[id], t)
		}
	}

	totalSeries := needSpace
	for _, r := range rt {
		r.Targets = newTargets
		totalSeries += r.HeadSeries
	}

	// need to increment new Shards
	needRep := int32(totalSeries/c.maxSeries + 1)
	if needRep > c.maxShard {
		needRep = c.maxShard
		c.lg.Warnf("need ShardsGroup = %d, but max = %d", needRep, c.maxShard)
	}

	c.lg.Infof("total series = %d, need ShardsGroup = %d", totalSeries, needRep)
	return needRep
}

// GlobalTargets combine all runtime info to get global Target set
func GlobalTargets(rs []*shard.RuntimeInfo) (map[string]map[string]*shard.Target, []*shard.Target) {
	retMap := make(map[string]map[string]*shard.Target)
	flag := map[string]bool{}
	retSlice := make([]*shard.Target, 0)
	for _, r := range rs {
		tMap := map[string]*shard.Target{}
		for _, t := range r.Targets[r.ID] {
			hash := t.Hash()
			if flag[hash] {
				continue
			}
			tMap[hash] = t
			flag[hash] = true
			retSlice = append(retSlice, t)
		}
		retMap[r.ID] = tMap
	}

	retMap[shard.IDUnknown] = map[string]*shard.Target{}
	for _, r := range rs {
		for _, t := range r.Targets[shard.IDUnknown] {
			hash := t.Hash()
			if !flag[hash] {
				retSlice = append(retSlice, t)
				retMap[shard.IDUnknown][hash] = t
			}
		}
	}

	return retMap, retSlice
}
