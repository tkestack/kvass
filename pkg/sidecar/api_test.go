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
package sidecar

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/prometheus/scrape"
	"github.com/sirupsen/logrus"
	"net/http"
	"net/http/httptest"
	"testing"
	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/test"
)

func TestAPI_ServeHTTP(t *testing.T) {
	var cases = []struct {
		name     string
		uri      string
		wantCall bool
	}{
		{
			name:     "want redirect",
			uri:      "/test2",
			wantCall: true,
		},
		{
			name:     "want handle",
			uri:      "/test",
			wantCall: false,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			promCalled := false
			tProm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				promCalled = true
				w.WriteHeader(200)
				return
			}))
			defer tProm.Close()

			a := NewAPI(tProm.URL, nil, nil, nil, logrus.New())
			a.Engine.Group(a.path("/test"), func(context *gin.Context) {})

			r := api.TestCall(t, a.ServeHTTP, cs.uri, http.MethodGet, "", nil)
			r.Equal(cs.wantCall, promCalled)
		})
	}
}

func TestAPI_runtimeInfo(t *testing.T) {
	var cases = []struct {
		name           string
		promRuntime    *prom.RuntimeInfo
		targetStatus   map[uint64]*target.ScrapeStatus
		wantHeadSeries int64
	}{
		{
			name:           "user prometheus runtime as head series info directly",
			promRuntime:    &prom.RuntimeInfo{TimeSeriesCount: 100},
			targetStatus:   map[uint64]*target.ScrapeStatus{},
			wantHeadSeries: 100,
		},
		{
			name:        "forecast head series if target has not begun to be scraped",
			promRuntime: &prom.RuntimeInfo{TimeSeriesCount: 0},
			targetStatus: map[uint64]*target.ScrapeStatus{
				1: {
					Health: scrape.HealthUnknown,
					Series: 100,
				},
			},
			wantHeadSeries: 100,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			a := NewAPI("", nil,
				func() (info *prom.RuntimeInfo, e error) {
					return cs.promRuntime, nil
				},
				func() map[uint64]*target.ScrapeStatus {
					return cs.targetStatus
				},
				logrus.New(),
			)

			res := &shard.RuntimeInfo{}
			r := api.TestCall(t, a.Engine.ServeHTTP, "/api/v1/shard/runtimeinfo/", http.MethodGet, "", res)
			r.Equal(cs.wantHeadSeries, res.HeadSeries)
		})
	}
}

func TestAPI_getTargets(t *testing.T) {
	want := map[uint64]*target.ScrapeStatus{
		1: {
			Health: scrape.HealthUnknown,
			Series: 100,
		},
	}

	a := NewAPI("", nil, nil,
		func() map[uint64]*target.ScrapeStatus {
			return want
		},
		logrus.New(),
	)

	res := map[uint64]*target.ScrapeStatus{}
	r := api.TestCall(t, a.Engine.ServeHTTP, "/api/v1/shard/targets/", http.MethodGet, "", &res)
	r.JSONEq(test.MustJSON(want), test.MustJSON(res))
}

func TestAPI_updateTargets(t *testing.T) {
	tar := map[string][]*target.Target{
		"job1": {
			{
				Series: 100,
				Hash:   1,
			},
		},
	}

	a := NewAPI("", nil, nil, nil, logrus.New())
	r := api.TestCall(t, a.Engine.ServeHTTP, "/api/v1/shard/targets/", http.MethodPost, test.MustJSON(tar), nil)
	res := <-a.TargetReload
	r.JSONEq(test.MustJSON(tar), test.MustJSON(res))
}
