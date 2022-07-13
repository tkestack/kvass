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

package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/target"
)

func TestTargetsDiscovery_WaitInit(t *testing.T) {
	cfg := &config.Config{
		ScrapeConfigs: []*config.ScrapeConfig{
			{
				JobName: "test",
			},
		},
	}

	var cases = []struct {
		name        string
		targets     map[string][]*SDTargets
		wantTimeout bool
	}{
		{
			name: "success",
			targets: map[string][]*SDTargets{
				"test": {},
			},
			wantTimeout: false,
		},
		{
			name:        "timeout",
			targets:     map[string][]*SDTargets{},
			wantTimeout: true,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			d := New(logrus.New())
			r.NoError(d.ApplyConfig(&prom.ConfigInfo{
				Config: cfg,
			}))
			d.activeTargets = cs.targets
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
			defer cancel()
			r.NoError(d.WaitInit(ctx))
			r.Equal(cs.wantTimeout, ctx.Err() != nil)
		})
	}

}
func TestTargetsDiscovery_ActiveTargets(t *testing.T) {
	d := New(logrus.New())
	d.activeTargets = map[string][]*SDTargets{
		"job": {
			{},
		},
	}
	require.Equal(t, 1, len(d.ActiveTargets()["job"]))
}

func TestTargetsDiscovery_ActiveTargetsByHash(t *testing.T) {
	d := New(logrus.New())
	d.activeTargets = map[string][]*SDTargets{
		"job": {
			{
				ShardTarget: &target.Target{Hash: 1},
			},
		},
	}
	require.Equal(t, 1, len(d.ActiveTargetsByHash()))
}

func TestTargetsDiscovery_DropTargets(t *testing.T) {
	d := New(logrus.New())
	d.dropTargets = map[string][]*SDTargets{
		"job": {
			{},
		},
	}
	require.Equal(t, 1, len(d.DropTargets()["job"]))
}

func TestTargetsDiscovery_Run(t *testing.T) {
	r := require.New(t)
	d := New(logrus.New())
	cfg := &config.Config{
		ScrapeConfigs: []*config.ScrapeConfig{
			{
				JobName: "test",
				Params: map[string][]string{
					"t1": {"v1"},
				},
				RelabelConfigs: []*relabel.Config{
					{
						Separator:   ";",
						Regex:       relabel.MustNewRegexp("__test_" + "(.+)"),
						Replacement: "$1",
						Action:      relabel.LabelMap,
					},
					{
						SourceLabels: model.LabelNames{"drop"},
						Regex:        relabel.MustNewRegexp("true"),
						Action:       relabel.Drop,
					},
				},
			},
		},
	}
	r.NoError(d.ApplyConfig(&prom.ConfigInfo{
		Config: cfg,
	}))

	sdChan := make(chan map[string][]*targetgroup.Group, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		r.NoError(d.Run(ctx, sdChan))
	}()

	d.activeTargetsChan = make(chan map[string][]*SDTargets)
	sdChan <- map[string][]*targetgroup.Group{
		cfg.ScrapeConfigs[0].JobName: {
			{
				Targets: []model.LabelSet{
					map[model.LabelName]model.LabelValue{
						model.MetricsPathLabel: "/metrics",
						model.SchemeLabel:      "https",
						model.AddressLabel:     "127.0.0.1",
						"__test_xx":            "xxx",
					},
				},
			},
			{
				Targets: []model.LabelSet{
					map[model.LabelName]model.LabelValue{
						model.AddressLabel: "127.0.0.2",
						"drop":             "true",
						model.SchemeLabel:  "http",
					},
				},
			},
		},
	}

	active := <-d.ActiveTargetsChan()
	job := cfg.ScrapeConfigs[0].JobName
	r.Equal(1, len(active[job]))
	tar := active[cfg.ScrapeConfigs[0].JobName][0]
	r.NotNil(tar.ShardTarget)
	r.NotNil(tar.PromTarget)

	r.Equal("xxx", tar.ShardTarget.Labels.Get("xx"))
	r.Equal(1, len(d.ActiveTargets()[job]))
	r.Equal("https://127.0.0.1:443/metrics?t1=v1", d.ActiveTargets()[job][0].PromTarget.URL().String())
	r.Equal(1, len(d.DropTargets()[job]))
}
