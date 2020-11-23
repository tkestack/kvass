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
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/test"
)

type fakeShardsManager struct {
	rtInfo       *shard.RuntimeInfo
	targetStatus map[uint64]*target.ScrapeStatus

	resultRep           int32
	resultUpdateTargets map[string][]*target.Target
}

// Shards return current Shards in the cluster
func (f *fakeShardsManager) Shards() ([]*shard.Group, error) {
	g := shard.NewGroup("shard-0", logrus.New())
	rep := shard.NewReplicas("r0", "", logrus.New())
	g.AddReplicas(rep)
	rep.APIGet = func(url string, ret interface{}) error {
		dm := map[string]interface{}{
			"/api/v1/shard/targets/":     f.targetStatus,
			"/api/v1/shard/runtimeinfo/": f.rtInfo,
		}
		return test.CopyJSON(ret, dm[url])
	}
	rep.APIPost = func(url string, req interface{}, ret interface{}) (err error) {
		f.resultUpdateTargets = map[string][]*target.Target{}
		return test.CopyJSON(&f.resultUpdateTargets, req)
	}
	return []*shard.Group{g}, nil
}

// ChangeScale create or delete Shards according to "expReplicate"
func (f *fakeShardsManager) ChangeScale(expReplicate int32) error {
	f.resultRep = expReplicate
	return nil
}

func TestCoordinator_RunOnce(t *testing.T) {
	job := "job1"
	tar := &target.Target{
		Hash:   1,
		Series: 100,
	}
	active := &discovery.SDTargets{
		ShardTarget: tar,
	}

	var cases = []struct {
		name              string
		maxSeries         int64
		maxShard          int32
		exploreResult     *target.ScrapeStatus
		shardManager      *fakeShardsManager
		wantRep           int32
		wantUpdateTargets map[string][]*target.Target
	}{
		{
			name:      "assign old target to shard already scrape it",
			maxSeries: 200,
			maxShard:  10,
			shardManager: &fakeShardsManager{
				rtInfo: &shard.RuntimeInfo{
					HeadSeries: 100,
				},
				// already scraping target
				targetStatus: map[uint64]*target.ScrapeStatus{
					1: {Health: scrape.HealthGood},
				},
			},
			wantRep:           1,
			wantUpdateTargets: nil, // scraping target is not changed
		},
		{
			name:      "assign new target to shard success",
			maxSeries: 200,
			maxShard:  10,
			shardManager: &fakeShardsManager{
				rtInfo: &shard.RuntimeInfo{
					HeadSeries: 0, // has enough space
				},
				// no scraping targets
				targetStatus: map[uint64]*target.ScrapeStatus{},
			},
			exploreResult: &target.ScrapeStatus{
				Series: 100,
				Health: scrape.HealthGood,
			},
			wantRep: 1,
			wantUpdateTargets: map[string][]*target.Target{
				job: {tar},
			},
		},
		{
			name:      "new target not explored",
			maxSeries: 200,
			maxShard:  10,
			shardManager: &fakeShardsManager{
				rtInfo: &shard.RuntimeInfo{
					HeadSeries: 0, // has enough space
				},
				// no scraping targets
				targetStatus: map[uint64]*target.ScrapeStatus{},
			},
			wantRep: 1,
		},
		{
			name:      "need scale up, and scale up success",
			maxSeries: 200,
			maxShard:  10,
			shardManager: &fakeShardsManager{
				rtInfo: &shard.RuntimeInfo{
					HeadSeries: 120, // has no enough space
				},
				// no scraping targets
				targetStatus: map[uint64]*target.ScrapeStatus{},
			},
			exploreResult: &target.ScrapeStatus{
				Series: 100,
				Health: scrape.HealthGood,
			},
			wantRep:           2,
			wantUpdateTargets: nil,
		},
		{
			name:      "need scale up, but max shard limited",
			maxSeries: 200,
			maxShard:  1,
			shardManager: &fakeShardsManager{
				rtInfo: &shard.RuntimeInfo{
					HeadSeries: 120, // has no enough space
				},
				// no scraping targets
				targetStatus: map[uint64]*target.ScrapeStatus{},
			},
			exploreResult: &target.ScrapeStatus{
				Series: 100,
				Health: scrape.HealthGood,
			},
			wantRep:           1,
			wantUpdateTargets: nil,
		},
		{
			name:      "delete target from shard if target not exist",
			maxSeries: 100,
			maxShard:  1,
			shardManager: &fakeShardsManager{
				rtInfo: &shard.RuntimeInfo{
					HeadSeries: 100, // has no enough space
				},
				// no scraping targets
				targetStatus: map[uint64]*target.ScrapeStatus{
					1: {Health: scrape.HealthGood},
					2: {Health: scrape.HealthGood},
				},
			},
			wantRep: 1,
			wantUpdateTargets: map[string][]*target.Target{
				job: {tar},
			},
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			c := NewCoordinator(
				cs.shardManager,
				cs.maxSeries,
				cs.maxShard, time.Second,
				func(job string, hash uint64) *target.ScrapeStatus {
					return cs.exploreResult
				},
				func() map[string][]*discovery.SDTargets {
					return map[string][]*discovery.SDTargets{
						job: {active},
					}
				},
				logrus.New(),
			)
			r.NoError(c.RunOnce())
			r.Equal(cs.wantRep, cs.shardManager.resultRep)
			r.Equal(test.MustJSON(cs.wantUpdateTargets), test.MustJSON(cs.shardManager.resultUpdateTargets))
		})
	}
}
