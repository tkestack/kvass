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
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/stretchr/testify/require"

	"tkestack.io/kvass/pkg/shard"
)

type fakeTargetManager struct {
	tar    map[string]*shard.Target
	sample int64
}

func newFakeTargetManager(sample int64) *fakeTargetManager {
	return &fakeTargetManager{
		tar:    map[string]*shard.Target{},
		sample: sample,
	}
}

func (f *fakeTargetManager) Update(all []*shard.Target) {
	for _, t := range all {
		f.tar[t.Hash()] = t
	}
	return
}

func (f *fakeTargetManager) Get(hash string) *Target {
	t := f.tar[hash]
	t.Samples = f.sample
	return newTarget(t)
}

func TestDefReBalance_ReBalance(t *testing.T) {
	maxSeries := int64(20)
	getTarget := func(url string) *shard.Target {
		return &shard.Target{
			JobName: "test",
			URL:     url,
			Samples: -1,
		}
	}

	getShards := func(id string, targets map[string][]*shard.Target) *shard.RuntimeInfo {
		return &shard.RuntimeInfo{
			ID:         id,
			HeadSeries: 5,
			Targets:    targets,
		}
	}

	var cases = []struct {
		name              string
		inputRt           []*shard.RuntimeInfo
		defSamples        int64
		wantTargetsNumber map[string]int
		wantReplicate     int32
	}{
		{
			name: "exploring target don't assign",
			inputRt: []*shard.RuntimeInfo{
				getShards("shard-0", map[string][]*shard.Target{
					shard.IDUnknown: {getTarget("t0")},
				}),
			},
			defSamples: -1,
			wantTargetsNumber: map[string]int{
				"shard-0":       0,
				shard.IDUnknown: 1,
			},
			wantReplicate: 1,
		},
		{
			name: "targets sharding, need scaling up",
			inputRt: []*shard.RuntimeInfo{
				getShards("shard-0", map[string][]*shard.Target{
					shard.IDUnknown: {getTarget("t0"), getTarget("t1"), getTarget("t2")},
				}),
				getShards("shard-1", map[string][]*shard.Target{}),
			},
			defSamples: maxSeries / 2,
			wantTargetsNumber: map[string]int{
				"shard-0":       1,
				"shard-1":       1,
				shard.IDUnknown: 1,
			},

			wantReplicate: 3,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			b := NewDefBalancer(maxSeries, 9999, newFakeTargetManager(cs.defSamples), logrus.New())
			r.Equal(cs.wantReplicate, b.ReBalance(cs.inputRt))
			for _, rt := range cs.inputRt {
				r.Equal(cs.wantTargetsNumber[rt.ID], len(cs.inputRt[0].Targets[rt.ID]), rt.ID)
				r.True(maxSeries > rt.HeadSeries, rt.ID)
			}
			r.Equal(cs.wantTargetsNumber[shard.IDUnknown], len(cs.inputRt[0].Targets[shard.IDUnknown]))
		})
	}
}

func TestGlobalTargets(t *testing.T) {
	t0 := &shard.Target{
		JobName: "test",
		URL:     "t0",
	}

	t1 := &shard.Target{
		JobName: "test",
		URL:     "t1",
	}

	t2 := &shard.Target{
		JobName: "test",
		URL:     "t2",
	}

	r0 := &shard.RuntimeInfo{
		ID:         "shard-0",
		HeadSeries: 5,
		Targets: map[string][]*shard.Target{
			"shard-0": {t0},
		},
	}

	r1 := &shard.RuntimeInfo{
		ID:         "shard-1",
		HeadSeries: 5,
		Targets: map[string][]*shard.Target{
			"shard-0": {},
			"shard-1": {t1},
		},
	}

	r2 := &shard.RuntimeInfo{
		ID:         "shard-2",
		HeadSeries: 5,
		Targets: map[string][]*shard.Target{
			shard.IDUnknown: {t0, t1, t2},
		},
	}

	m, s := GlobalTargets([]*shard.RuntimeInfo{r0, r1, r2})
	require.Equal(t, 3, len(s))

	require.Equal(t, 1, len(m[r0.ID]))
	require.Equal(t, "t0", m[r0.ID][t0.Hash()].URL)

	require.Equal(t, 1, len(m[r1.ID]))
	require.Equal(t, "t1", m[r1.ID][t1.Hash()].URL)

	require.Equal(t, 1, len(m[shard.IDUnknown]))
	require.Equal(t, "t2", m[shard.IDUnknown][t2.Hash()].URL)
}
