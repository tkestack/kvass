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
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/test"
	"tkestack.io/kvass/pkg/utils/types"
)

type testingShard struct {
	rtInfo        *shard.RuntimeInfo
	targetStatus  map[uint64]*target.ScrapeStatus
	resultTargets shard.UpdateTargetsRequest
	wantTargets   shard.UpdateTargetsRequest
}

func (ts *testingShard) assert(t *testing.T) {
	require.JSONEq(t, test.MustJSON(ts.wantTargets), test.MustJSON(ts.resultTargets))
}

type fakeReplicasManager struct {
	fd *fakeShardsManager
}

func (f *fakeReplicasManager) Replicas() ([]shard.Manager, error) {
	return []shard.Manager{f.fd}, nil
}

type fakeShardsManager struct {
	resultRep int32
	wantRep   int32
	shards    []*testingShard
}

// Shards return current Shards in the cluster
func (f *fakeShardsManager) Shards() ([]*shard.Shard, error) {
	ret := make([]*shard.Shard, 0)
	for i, s := range f.shards {
		sd := shard.NewShard(fmt.Sprint(i)+"-r0", "", true, logrus.New())
		temp := s
		sd.APIGet = func(url string, ret interface{}) error {
			dm := map[string]interface{}{
				"/api/v1/shard/targets/":     temp.targetStatus,
				"/api/v1/shard/runtimeinfo/": temp.rtInfo,
			}
			return test.CopyJSON(ret, dm[url])
		}

		sd.APIPost = func(url string, req interface{}, ret interface{}) (err error) {
			return test.CopyJSON(&temp.resultTargets, req)
		}

		ret = append(ret, sd)
	}

	return ret, nil
}

// ChangeScale create or delete Shards according to "expReplicate"
func (f *fakeShardsManager) ChangeScale(expReplicate int32) error {
	f.resultRep = expReplicate
	return nil
}

func (f *fakeShardsManager) assert(t *testing.T) {
	r := require.New(t)
	r.Equal(f.wantRep, f.resultRep)

	rs := f.shards
	if f.resultRep < int32(len(f.shards)) {
		rs = f.shards[:int(f.resultRep)]
	}

	for _, r := range rs {
		r.assert(t)
	}
}

func TestCoordinator_RunOnce(t *testing.T) {
	var cases = []struct {
		name             string
		maxSeries        int64
		maxShard         int32
		minShard         int32
		maxIdleTime      time.Duration
		period           time.Duration
		getExploreResult func(hash uint64) *target.ScrapeStatus
		getActive        func() map[uint64]*discovery.SDTargets
		shardManager     *fakeShardsManager
	}{
		{
			name:        "delete not exist target",
			maxSeries:   1000,
			maxShard:    1000,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{}
			},
			shardManager: &fakeShardsManager{
				wantRep: 1,
				shards: []*testingShard{
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 1,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{
							1: {},
						},
						wantTargets: shard.UpdateTargetsRequest{Targets: map[string][]*target.Target{}},
					},
				},
			},
		},
		{
			name:        "assign new target normally",
			maxSeries:   1000,
			maxShard:    1000,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{
					1: {
						Job: "test",
						ShardTarget: &target.Target{
							Hash: 1,
						},
					},
				}
			},
			getExploreResult: func(hash uint64) *target.ScrapeStatus {
				return &target.ScrapeStatus{Series: 10, Health: scrape.HealthGood}
			},
			shardManager: &fakeShardsManager{
				wantRep: 1,
				shards: []*testingShard{
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 1,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{},
						wantTargets: shard.UpdateTargetsRequest{
							Targets: map[string][]*target.Target{
								"test": {
									{
										Hash:        1,
										Series:      10,
										TargetState: target.StateNormal,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:        "assign new target, shard not changeable, don't scale up",
			maxSeries:   1000,
			maxShard:    1000,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{
					1: {
						Job: "test",
						ShardTarget: &target.Target{
							Hash: 1,
						},
					},
				}
			},
			getExploreResult: func(hash uint64) *target.ScrapeStatus {
				return &target.ScrapeStatus{Series: 10, Health: scrape.HealthGood}
			},
			shardManager: &fakeShardsManager{
				wantRep: 1,
				shards: []*testingShard{
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 1,
							ConfigHash: "invalid",
						},
						wantTargets: shard.UpdateTargetsRequest{
							// shard not changeable , don't assign
						},
					},
				},
			},
		},
		{
			name:        "assign new target, need scale up, but max shard limit ",
			maxSeries:   1000,
			maxShard:    0,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{
					1: {
						Job: "test",
						ShardTarget: &target.Target{
							Hash: 1,
						},
					},
				}
			},
			getExploreResult: func(hash uint64) *target.ScrapeStatus {
				return &target.ScrapeStatus{Series: 10, Health: scrape.HealthGood}
			},
			shardManager: &fakeShardsManager{
				wantRep: 0,
			},
		},
		{
			name:        "assign new target, need scale up",
			maxSeries:   1000,
			maxShard:    1000,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{
					1: {
						Job: "test",
						ShardTarget: &target.Target{
							Hash: 1,
						},
					},
				}
			},
			getExploreResult: func(hash uint64) *target.ScrapeStatus {
				return &target.ScrapeStatus{Series: 10, Health: scrape.HealthGood}
			},
			shardManager: &fakeShardsManager{
				wantRep: 1,
			},
		},
		{
			name:        "shard overload, no space, need scale up",
			maxSeries:   1000,
			maxShard:    1000,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{
					1: {
						Job: "test",
						ShardTarget: &target.Target{
							Hash: 1,
						},
					},
				}
			},
			shardManager: &fakeShardsManager{
				wantRep: 2,
				shards: []*testingShard{
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 2000,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{
							1: {
								Series:      100,
								Health:      scrape.HealthGood,
								ScrapeTimes: minWaitScrapeTimes,
							},
						},
						wantTargets: shard.UpdateTargetsRequest{
							// targets no change
						},
					},
				},
			},
		},
		{
			name:        "shard overload, transfer begin",
			maxSeries:   1000,
			maxShard:    1000,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{
					1: {
						Job: "test",
						ShardTarget: &target.Target{
							Hash: 1,
						},
					},
				}
			},
			shardManager: &fakeShardsManager{
				wantRep: 2,
				shards: []*testingShard{
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 2000,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{
							1: {
								Series:      100,
								Health:      scrape.HealthGood,
								ScrapeTimes: minWaitScrapeTimes,
							},
						},
						wantTargets: shard.UpdateTargetsRequest{
							Targets: map[string][]*target.Target{
								"test": {
									{
										Hash:        1,
										Series:      100,
										TargetState: target.StateInTransfer,
									},
								},
							},
						},
					},
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 10,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{},
						wantTargets: shard.UpdateTargetsRequest{
							Targets: map[string][]*target.Target{
								"test": {
									{
										Hash:        1,
										Series:      100,
										TargetState: target.StateNormal,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:        "shard overload, transfer end",
			maxSeries:   1000,
			maxShard:    1000,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{
					1: {
						Job: "test",
						ShardTarget: &target.Target{
							Hash: 1,
						},
					},
				}
			},
			shardManager: &fakeShardsManager{
				wantRep: 2,
				shards: []*testingShard{
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 2000,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{
							1: {
								Series:      100,
								Health:      scrape.HealthGood,
								ScrapeTimes: minWaitScrapeTimes,
								TargetState: target.StateInTransfer,
							},
						},
						wantTargets: shard.UpdateTargetsRequest{
							Targets: map[string][]*target.Target{}, // delete target
						},
					},
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 10,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{
							1: {
								Series:      100,
								Health:      scrape.HealthGood,
								ScrapeTimes: minWaitScrapeTimes,
								TargetState: target.StateNormal,
							},
						},
						wantTargets: shard.UpdateTargetsRequest{
							// nothing changed
						},
					},
				},
			},
		},
		{
			name:        "shard can be removed, transfer begin",
			maxSeries:   1000,
			maxShard:    1000,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{
					1: {
						Job: "test",
						ShardTarget: &target.Target{
							Hash: 1,
						},
					},
				}
			},
			shardManager: &fakeShardsManager{
				wantRep: 2,
				shards: []*testingShard{
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 10,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{},
						wantTargets: shard.UpdateTargetsRequest{
							Targets: map[string][]*target.Target{
								"test": {
									{
										Hash:        1,
										Series:      100,
										TargetState: target.StateNormal,
									},
								},
							},
						},
					},
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 100,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{
							1: {
								Series:      100,
								Health:      scrape.HealthGood,
								ScrapeTimes: minWaitScrapeTimes,
								TargetState: target.StateNormal,
							},
						},
						wantTargets: shard.UpdateTargetsRequest{
							Targets: map[string][]*target.Target{
								"test": {
									{
										Hash:        1,
										Series:      100,
										TargetState: target.StateInTransfer,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:        "shard can be removed, transfer end, begin idle",
			maxSeries:   1000,
			maxShard:    1000,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{
					1: {
						Job: "test",
						ShardTarget: &target.Target{
							Hash: 1,
						},
					},
				}
			},
			shardManager: &fakeShardsManager{
				wantRep: 2,
				shards: []*testingShard{
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 10,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{
							1: {
								Series:      100,
								Health:      scrape.HealthGood,
								ScrapeTimes: minWaitScrapeTimes,
								TargetState: target.StateNormal,
							},
						},
						wantTargets: shard.UpdateTargetsRequest{
							// nothing changed
						},
					},
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 100,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{
							1: {
								Series:      100,
								Health:      scrape.HealthGood,
								ScrapeTimes: minWaitScrapeTimes,
								TargetState: target.StateInTransfer,
							},
						},
						wantTargets: shard.UpdateTargetsRequest{
							Targets: map[string][]*target.Target{}, // target deleted
						},
					},
				},
			},
		},
		{
			name:        "shard can be removed, transfer end, shard removed",
			maxSeries:   1000,
			maxShard:    1000,
			maxIdleTime: time.Second,
			getActive: func() map[uint64]*discovery.SDTargets {
				return map[uint64]*discovery.SDTargets{
					1: {
						Job: "test",
						ShardTarget: &target.Target{
							Hash: 1,
						},
					},
				}
			},
			shardManager: &fakeShardsManager{
				wantRep: 1, // must scale down
				shards: []*testingShard{
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries: 10,
						},
						targetStatus: map[uint64]*target.ScrapeStatus{
							1: {
								Series:      100,
								Health:      scrape.HealthGood,
								ScrapeTimes: minWaitScrapeTimes,
								TargetState: target.StateNormal,
							},
						},
						wantTargets: shard.UpdateTargetsRequest{
							// nothing changed
						},
					},
					{
						rtInfo: &shard.RuntimeInfo{
							HeadSeries:  100,
							IdleStartAt: types.TimePtr(time.Now().Add(-time.Second * 3)),
						},
						targetStatus: map[uint64]*target.ScrapeStatus{},
						wantTargets:  shard.UpdateTargetsRequest{},
					},
				},
			},
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			c := NewCoordinator(&fakeReplicasManager{cs.shardManager}, cs.maxSeries, cs.maxShard, cs.minShard, cs.maxIdleTime, cs.period, func() string {
				return ""
			}, cs.getExploreResult, cs.getActive, logrus.New())
			require.NoError(t, c.runOnce())
			cs.shardManager.assert(t)
		})
	}
}

func TestCoordinator_LastGlobalScrapeStatus(t *testing.T) {
	getStatus := func(hash uint64) *target.ScrapeStatus {
		return &target.ScrapeStatus{Series: 1, Health: scrape.HealthGood}
	}

	shardManager := &fakeShardsManager{
		shards: []*testingShard{
			{
				rtInfo: &shard.RuntimeInfo{
					HeadSeries: 100,
				},
				targetStatus: map[uint64]*target.ScrapeStatus{
					2: {
						Series: 100,
						Health: scrape.HealthBad,
					},
				},
			},
		},
	}

	active := func() map[uint64]*discovery.SDTargets {
		return map[uint64]*discovery.SDTargets{
			1: {
				Job: "test",
				ShardTarget: &target.Target{
					Hash: 1,
				},
			},
			2: {
				Job: "test",
				ShardTarget: &target.Target{
					Hash: 2,
				},
			},
		}
	}

	c := NewCoordinator(&fakeReplicasManager{shardManager}, 100, 100, 100, 0, time.Second, func() string {
		return ""
	}, getStatus, active, logrus.New())

	r := require.New(t)
	r.NoError(c.runOnce())
	g := c.LastGlobalScrapeStatus()
	r.NotNil(g[1])
	r.NotNil(g[2])
}
