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

package shardmanager

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sirupsen/logrus"

	v1 "github.com/prometheus/prometheus/web/api/v1"
	"tkestack.io/kvass/pkg/shard"
)

type fakeShardClient struct {
	r       *shard.RuntimeInfo
	wantErr bool
}

func newFakeShardClient(r *shard.RuntimeInfo, wantErr bool) *fakeShardClient {
	return &fakeShardClient{
		r:       r,
		wantErr: wantErr,
	}
}

func (f *fakeShardClient) RuntimeInfo() (*shard.RuntimeInfo, error) {
	if f.wantErr {
		return nil, fmt.Errorf("test")
	}
	return f.r, nil
}

func (f *fakeShardClient) UpdateRuntimeInfo(r *shard.RuntimeInfo) error {
	if f.wantErr {
		return fmt.Errorf("test")
	}

	f.r = r
	return nil
}

func (f *fakeShardClient) Targets(state string) (*v1.TargetDiscovery, error) {
	if f.wantErr {
		return nil, fmt.Errorf("test")
	}
	return &v1.TargetDiscovery{}, nil
}

func TestPodsGroup_RuntimeInfo(t *testing.T) {
	var cases = []struct {
		name    string
		rts     []*shard.RuntimeInfo
		wantRt  *shard.RuntimeInfo
		wantErr bool
	}{
		{
			name: "all pods failed",
			rts: []*shard.RuntimeInfo{
				nil, nil,
			},
			wantRt:  nil,
			wantErr: true,
		},
		{
			name: "at least one success",
			rts: []*shard.RuntimeInfo{
				nil,
				{
					ID:         "shard-0",
					HeadSeries: 5,
				},
			},
			wantRt: &shard.RuntimeInfo{
				ID:         "shard-0",
				HeadSeries: 5,
			},
			wantErr: false,
		},
		{
			name: "select max series one",
			rts: []*shard.RuntimeInfo{
				{
					ID:         "shard-0",
					HeadSeries: 5,
				},
				{
					ID:         "shard-0",
					HeadSeries: 10,
				},
			},
			wantRt: &shard.RuntimeInfo{
				ID:         "shard-0",
				HeadSeries: 10,
			},
			wantErr: false,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			pg := newPodsGroup("shard-0", logrus.New())
			for i, r := range cs.rts {
				f := newFakeShardClient(r, r == nil)
				pg.pods = append(pg.pods, &pod{
					name: fmt.Sprint(i),
					cli:  f,
				})
			}

			rt, err := pg.RuntimeInfo()
			if cs.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Equal(cs.wantRt.HeadSeries, rt.HeadSeries)
		})
	}
}
func TestPodsGroup_UpdateRuntimeInfo(t *testing.T) {
	var cases = []struct {
		name    string
		cliErr  []bool
		wantErr bool
	}{
		{
			name:    "all pods failed",
			cliErr:  []bool{true, true},
			wantErr: true,
		},
		{
			name:    "at least one success",
			cliErr:  []bool{true, false},
			wantErr: false,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			pg := newPodsGroup("shard-0", logrus.New())
			for i, r := range cs.cliErr {
				f := newFakeShardClient(nil, r)
				pg.pods = append(pg.pods, &pod{
					name: fmt.Sprint(i),
					cli:  f,
				})
			}

			err := pg.UpdateRuntimeInfo(&shard.RuntimeInfo{})
			if cs.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
		})
	}
}

func TestPodsGroup_Targets(t *testing.T) {
	var cases = []struct {
		name    string
		cliErr  []bool
		wantErr bool
	}{
		{
			name:    "all pods failed",
			cliErr:  []bool{true, true},
			wantErr: true,
		},
		{
			name:    "at least one success",
			cliErr:  []bool{true, false},
			wantErr: false,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			pg := newPodsGroup("shard-0", logrus.New())
			for i, r := range cs.cliErr {
				f := newFakeShardClient(nil, r)
				pg.pods = append(pg.pods, &pod{
					name: fmt.Sprint(i),
					cli:  f,
				})
			}

			_, err := pg.Targets("")
			if cs.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
		})
	}
}
