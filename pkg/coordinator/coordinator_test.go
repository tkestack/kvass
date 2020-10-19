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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sirupsen/logrus"

	v1 "github.com/prometheus/prometheus/web/api/v1"
	"tkestack.io/kvass/pkg/shard"
)

type fakeShardClient struct {
	r *shard.RuntimeInfo
	t *v1.TargetDiscovery
}

func newFakeShardClient(r *shard.RuntimeInfo) *fakeShardClient {
	return &fakeShardClient{r: r}
}

func (f *fakeShardClient) RuntimeInfo() (*shard.RuntimeInfo, error) {
	return f.r, nil
}
func (f *fakeShardClient) UpdateRuntimeInfo(r *shard.RuntimeInfo) error {
	f.r = r
	return nil
}
func (f *fakeShardClient) Targets(state string) (*v1.TargetDiscovery, error) {
	return f.t, nil
}

type fakeShardsManager struct {
	s   []shard.Client
	rep int32
}

func newFakeShardsManager(s []shard.Client) *fakeShardsManager {
	return &fakeShardsManager{s: s}
}

func (f *fakeShardsManager) Shards() ([]shard.Client, error) {
	return f.s, nil
}

func (f *fakeShardsManager) ChangeScale(expReplicate int32) error {
	f.rep = expReplicate
	return nil
}

func TestCoordinator_Run(t *testing.T) {
	tr1 := &shard.Target{
		JobName: "test",
		URL:     "t1",
	}

	s1 := newFakeShardClient(&shard.RuntimeInfo{
		ID:         "shard-0",
		HeadSeries: 5,
		Targets: map[string][]*shard.Target{
			"shard-0": {tr1},
		},
	})

	r := require.New(t)
	sm := newFakeShardsManager([]shard.Client{s1})

	ctx, cancel := context.WithCancel(context.Background())
	c := NewCoordinator(sm, func(rt []*shard.RuntimeInfo) int32 {
		r.Equal(1, len(rt))
		r.NotNil(rt[0].Targets)
		cancel()
		return 10
	}, time.Millisecond*10, logrus.New())

	r.NoError(c.Run(ctx))
	r.Equal(int32(10), sm.rep)
}
