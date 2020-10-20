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

package shard

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/prometheus/prometheus/config"

	"tkestack.io/kvass/pkg/utils/test"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/scrape"

	v1 "github.com/prometheus/prometheus/web/api/v1"
	"tkestack.io/kvass/pkg/prom"
)

type fakePromCli struct {
	prom.Client
	r *prom.RuntimeInfo
	a *v1.TargetDiscovery
}

func (f *fakePromCli) RuntimeInfo() (*prom.RuntimeInfo, error) {
	return f.r, nil
}

func (f *fakePromCli) Targets(state string) (*v1.TargetDiscovery, error) {
	if state == "active" {
		return &v1.TargetDiscovery{
			ActiveTargets: f.a.ActiveTargets,
		}, nil
	}
	return f.a, nil
}

func TestRuntimeManager_RuntimeInfo(t *testing.T) {
	localTarget := &Target{
		JobName: "test",
		URL:     "http://127.0.0.1:9090",
		Healthy: "down",
		Samples: 10,
		shardID: "shard-0",
	}

	promTarget := &v1.Target{
		DiscoveredLabels: map[string]string{"job": localTarget.JobName},
		ScrapeURL:        fmt.Sprintf("%s?%s=%s", localTarget.URL, jobNameFormName, "test"),
		Health:           "up",
	}

	promRuntime := &prom.RuntimeInfo{TimeSeriesCount: 5}

	var cases = []struct {
		name            string
		shardID         string
		targetNotFound  bool
		wantRuntimeInfo *RuntimeInfo
	}{
		{
			name:    "scraping target, Health eq to promTarget ",
			shardID: localTarget.shardID,
			wantRuntimeInfo: &RuntimeInfo{
				ID:         localTarget.shardID,
				HeadSeries: 15, // should add never scraped Target's HeadSeries
				Targets: map[string][]*Target{
					localTarget.shardID: {
						{
							JobName: localTarget.JobName,
							URL:     localTarget.URL,
							Healthy: string(promTarget.Health), // should eq to promTarget
							Samples: localTarget.Samples,
						},
					},
				},
			},
		},
		{
			name:    "not scraping target, Health eq to localTarget",
			shardID: localTarget.shardID + "not",
			wantRuntimeInfo: &RuntimeInfo{
				ID:         localTarget.shardID + "not",
				HeadSeries: 5, // only return prom report HeadSeries
				Targets: map[string][]*Target{
					localTarget.shardID: {localTarget},
				},
			},
		},
		{
			name:           "not found target, Health should be unknown and should saved in IDUnknown",
			shardID:        localTarget.shardID,
			targetNotFound: true,
			wantRuntimeInfo: &RuntimeInfo{
				ID:         localTarget.shardID,
				HeadSeries: 5, // only return prom report HeadSeries
				Targets: map[string][]*Target{
					IDUnknown: {
						{
							JobName: localTarget.JobName,
							URL:     localTarget.URL,
							Healthy: string(scrape.HealthUnknown), // should eq to promTarget
							Samples: -1,
						},
					},
				},
			},
		},
	}
	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			promCli := &fakePromCli{
				r: promRuntime,
				a: &v1.TargetDiscovery{
					ActiveTargets: []*v1.Target{promTarget},
				},
			}
			tm := NewTargetManager(logrus.New())
			if !cs.targetNotFound {
				tmp := *localTarget
				tm.Update(map[string][]*Target{
					localTarget.shardID: {
						&tmp,
					},
				}, cs.shardID)
			}

			rt := NewRuntimeManager(tm, promCli, logrus.New())
			rt.cur.ID = cs.shardID
			rt.ScrapeInfos = prom.NewScrapInfos(map[string]*prom.ScrapeInfo{
				localTarget.JobName: {
					Client: http.DefaultClient,
					Config: &config.ScrapeConfig{
						JobName: localTarget.JobName,
						Scheme:  "http",
					},
				},
			})
			info, err := rt.RuntimeInfo()
			r.NoError(err)
			r.JSONEq(test.MustJSON(cs.wantRuntimeInfo), test.MustJSON(info))
		})
	}
}

func TestRuntimeManager_Update(t *testing.T) {
	r := require.New(t)
	tm := NewTargetManager(logrus.New())
	rt := NewRuntimeManager(tm, &fakePromCli{}, logrus.New())

	info := &RuntimeInfo{
		ID:         "shard-0",
		HeadSeries: 10,
		Targets: map[string][]*Target{
			"shard-0": {
				{
					JobName: "test",
					URL:     "url",
				},
			},
		},
	}
	r.NoError(rt.Update(info))
	r.JSONEq(test.MustJSON(info), test.MustJSON(rt.cur))
	r.NotNil(tm.Get("test", "url"))
}

func TestRuntimeManager_Targets(t *testing.T) {
	act := &v1.Target{
		DiscoveredLabels: map[string]string{
			"job":              "test",
			"__scheme__":       "http",
			"__param__jobName": "test",
		},
		ScrapePool: "test",
		ScrapeURL:  "http://127.0.0.1:9090?_jobName=test",
		GlobalURL:  "http://localhost:9090?_jobName=test",
		Health:     "down",
	}
	dct := &v1.DroppedTarget{
		DiscoveredLabels: map[string]string{
			"__scheme__":       "http",
			"__param__jobName": "test",
		},
	}
	localT := &Target{
		JobName: "test",
		URL:     "https://127.0.0.1:9090",
		Healthy: "down",
		lastErr: "testErr",
	}

	promCli := &fakePromCli{
		a: &v1.TargetDiscovery{
			ActiveTargets:  []*v1.Target{act},
			DroppedTargets: []*v1.DroppedTarget{dct},
		},
	}
	tm := NewTargetManager(logrus.New())
	tm.Update(map[string][]*Target{
		"shard-0": {localT},
	}, "shard-0")

	rt := NewRuntimeManager(tm, promCli, logrus.New())
	rt.ScrapeInfos = prom.NewScrapInfos(map[string]*prom.ScrapeInfo{
		"test": {
			Config: &config.ScrapeConfig{
				JobName: "test",
				Scheme:  "https",
			},
		},
	})
	ts, err := rt.Targets("all")
	require.NoError(t, err)

	require.Equal(t, 1, len(ts.ActiveTargets))
	require.Equal(t, 1, len(ts.DroppedTargets))

	act = ts.ActiveTargets[0]
	require.Equal(t, localT.URL, act.ScrapeURL)
	require.Equal(t, "https://localhost:9090", act.GlobalURL)
	require.Equal(t, "https", act.DiscoveredLabels["__scheme__"])
	require.Empty(t, act.DiscoveredLabels["__param__jobName"])
	require.Equal(t, localT.lastErr, act.LastError)

	dct = ts.DroppedTargets[0]
	require.Equal(t, "https", act.DiscoveredLabels["__scheme__"])
	require.Empty(t, dct.DiscoveredLabels["__param__jobName"])
}
