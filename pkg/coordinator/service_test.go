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
	"net/http"
	"net/url"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/scrape"
	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/test"
)

func TestAPI_Targets(t *testing.T) {
	lbs := labels.Labels{
		{
			Name:  model.AddressLabel,
			Value: "127.0.0.1:80",
		},
		{
			Name:  model.SchemeLabel,
			Value: "http",
		},
	}

	getDrop := func() map[string][]*discovery.SDTargets {
		return map[string][]*discovery.SDTargets{
			"job1": {
				{
					PromTarget: scrape.NewTarget(lbs, lbs, url.Values{}),
				},
			},
		}
	}

	getScrapeStatus := func() map[uint64]*target.ScrapeStatus {
		return map[uint64]*target.ScrapeStatus{
			1: {
				Health:    scrape.HealthBad,
				LastError: "test",
			},
		}
	}

	getActive := func() map[string][]*discovery.SDTargets {
		return map[string][]*discovery.SDTargets{
			"job1": {
				{
					ShardTarget: &target.Target{
						Hash:   1,
						Labels: lbs,
						Series: 0,
					},
					PromTarget: scrape.NewTarget(lbs, lbs, url.Values{}),
				},
			},
		}
	}

	var cases = []struct {
		name           string
		param          url.Values
		wantActive     int
		wantDropped    int
		wantStatistics []TargetStatistics
	}{
		{
			name:        "(state=): return all targets only",
			wantActive:  1,
			wantDropped: 1,
		},
		{
			name: "(state=any): return all targets",
			param: url.Values{
				"state": []string{"any"},
			},
			wantActive:  1,
			wantDropped: 1,
		},
		{
			name: "(state=active): return active targets only",
			param: url.Values{
				"state": []string{"active"},
			},
			wantActive:  1,
			wantDropped: 0,
		},
		{
			name: "(state=dropped): return dropped targets only",
			param: url.Values{
				"state": []string{"dropped"},
			},
			wantActive:  0,
			wantDropped: 1,
		},
		{
			name: "(statistics=only): return statistics only",
			param: url.Values{
				"statistics": []string{"only"},
			},
			wantActive:  0,
			wantDropped: 0,
			wantStatistics: []TargetStatistics{
				{
					JobName: "job1",
					Total:   1,
					Health: map[scrape.TargetHealth]uint64{
						scrape.HealthBad: 1,
					},
				},
			},
		},
		{
			name: "(statistics=with): return statistics and all targets",
			param: url.Values{
				"statistics": []string{"with"},
			},
			wantActive:  1,
			wantDropped: 1,
			wantStatistics: []TargetStatistics{
				{
					JobName: "job1",
					Total:   1,
					Health: map[scrape.TargetHealth]uint64{
						scrape.HealthBad: 1,
					},
				},
			},
		},
		{
			name: "(statistics=with,state=active): return statistics and active targets",
			param: url.Values{
				"statistics": []string{"with"},
				"state":      []string{"active"},
			},
			wantActive:  1,
			wantDropped: 0,
			wantStatistics: []TargetStatistics{
				{
					JobName: "job1",
					Total:   1,
					Health: map[scrape.TargetHealth]uint64{
						scrape.HealthBad: 1,
					},
				},
			},
		},
		{
			name: "(job=job.*,state=active): return targets with job_name",
			param: url.Values{
				"job":   []string{"job.*"},
				"state": []string{"active"},
			},
			wantActive:  1,
			wantDropped: 0,
		},
		{
			name: "(job=xx,state=active): not targets returned with wrong job_name",
			param: url.Values{
				"job":   []string{"xx"},
				"state": []string{"active"},
			},
			wantActive:  0,
			wantDropped: 0,
		},
		{
			name: "(health=down,state=active): return targets with special health",
			param: url.Values{
				"health": []string{"down"},
				"state":  []string{"active"},
			},
			wantActive:  1,
			wantDropped: 0,
		},
		{
			name: "(health=down,up,state=active): return targets with muti special health",
			param: url.Values{
				"health": []string{"down", "up"},
				"state":  []string{"active"},
			},
			wantActive:  1,
			wantDropped: 0,
		},
		{
			name: "(health=up,state=active): not targets returned with wrong health",
			param: url.Values{
				"health": []string{"up"},
				"state":  []string{"active"},
			},
			wantActive:  0,
			wantDropped: 0,
		},
	}
	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			a := NewService("", prom.NewConfigManager(), nil, getScrapeStatus, getActive, getDrop,
				prometheus.NewRegistry(), logrus.New())
			uri := "/api/v1/targets"
			if len(cs.param) != 0 {
				uri += "?" + cs.param.Encode()
			}

			res := &TargetDiscovery{}
			r, _ := api.TestCall(t, a.Engine.ServeHTTP, uri, http.MethodGet, "", res)
			r.Equal(cs.wantActive, len(res.ActiveTargets))
			r.Equal(cs.wantDropped, len(res.DroppedTargets))
			r.JSONEq(test.MustJSON(cs.wantStatistics), test.MustJSON(res.ActiveStatistics))
		})
	}
}

func TestAPI_RuntimeInfo(t *testing.T) {
	a := NewService("", prom.NewConfigManager(), nil, func() map[uint64]*target.ScrapeStatus {
		return map[uint64]*target.ScrapeStatus{
			1: {
				Series: 100,
			},
			2: {
				Series: 100,
			},
		}
	}, nil, nil, prometheus.NewRegistry(), logrus.New())
	res := &shard.RuntimeInfo{}
	r, _ := api.TestCall(t, a.Engine.ServeHTTP, "/api/v1/runtimeinfo", http.MethodGet, "", res)
	r.Equal(int64(200), res.HeadSeries)
}
