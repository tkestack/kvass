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
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/scrape"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"testing"
	"time"
	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/test"
)

const configTest = `
global:
  scrape_interval:     15s # Set the scrape interval to every 15 seconds. Default is every 1 minute.
  evaluation_interval: 15s # Evaluate rules every 15 seconds. The default is every 1 minute.
`

func tempConfigFile(dir string) string {
	file := path.Join(dir, "config")
	if err := ioutil.WriteFile(file, []byte(configTest), 0755); err != nil {
		panic(err)
	}
	return file
}

func tempTargetStore(r *shard.UpdateTargetsRequest, dir string, old bool) string {
	var (
		file = ""
		data = []byte{}
		err  error
	)

	if old {
		file = "targets.json"
		data, err = json.Marshal(r.Targets)
		if err != nil {
			panic(err)
		}
	} else {
		file = "kvass-shard.json"
		data, err = json.Marshal(r)
		if err != nil {
			panic(err)
		}
	}

	if err := ioutil.WriteFile(path.Join(dir, file), data, 0755); err != nil {
		panic(err)
	}
	return dir
}

func TestService_Init(t *testing.T) {
	testServiceInit(t, true)
	testServiceInit(t, false)
}

func testServiceInit(t *testing.T, old bool) {
	r := require.New(t)
	configLoad := false
	targetsLoad := false
	configFile := tempConfigFile(t.TempDir())
	rq := &shard.UpdateTargetsRequest{
		Targets: map[string][]*target.Target{
			"job1": {{
				Hash: 1,
			}},
		},
	}

	s := NewService("", configFile, tempTargetStore(rq, t.TempDir(), old),
		[]func(*config.Config) error{
			func(i *config.Config) error {
				configLoad = true
				return nil
			},
		},
		[]func(map[string][]*target.Target) error{
			func(targets map[string][]*target.Target) error {
				targetsLoad = true
				r.Equal(test.MustJSON(rq.Targets), test.MustJSON(targets))
				return nil
			},
		},
		nil, nil, logrus.New(),
	)
	r.NoError(s.Init())
	r.True(configLoad)
	r.True(targetsLoad)
}

func TestService_ServeHTTP(t *testing.T) {
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

			a := NewService(tProm.URL, "", "", nil, nil, nil, nil, logrus.New())
			a.ginEngine.POST(a.path("/test"), func(context *gin.Context) {})

			r := api.TestCall(t, a.ServeHTTP, cs.uri, http.MethodGet, "", nil)
			r.Equal(cs.wantCall, promCalled)
		})
	}
}

func TestService_runtimeInfo(t *testing.T) {
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
			wantHeadSeries: 0,
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
			wantHeadSeries: 0,
		},
		{
			name:        "forecast head series if target has not begun to be scraped",
			promRuntime: &prom.RuntimeInfo{TimeSeriesCount: 200},
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
			a := NewService("", "", "", nil, nil,
				func() (info *prom.RuntimeInfo, e error) {
					return cs.promRuntime, nil
				},
				func() map[uint64]*target.ScrapeStatus {
					return cs.targetStatus
				},
				logrus.New(),
			)
			a.lastReBalanceAt = time.Now()

			res := &shard.RuntimeInfo{}
			r := api.TestCall(t, a.ginEngine.ServeHTTP, "/api/v1/shard/runtimeinfo/", http.MethodGet, "", res)
			r.Equal(cs.wantHeadSeries, res.HeadSeries)
		})
	}
}

func TestService_getTargets(t *testing.T) {
	want := map[uint64]*target.ScrapeStatus{
		1: {
			Health: scrape.HealthUnknown,
			Series: 100,
		},
	}

	a := NewService("", "", "", nil, nil, nil,
		func() map[uint64]*target.ScrapeStatus {
			return want
		},
		logrus.New(),
	)

	res := map[uint64]*target.ScrapeStatus{}
	r := api.TestCall(t, a.ginEngine.ServeHTTP, "/api/v1/shard/targets/", http.MethodGet, "", &res)
	r.JSONEq(test.MustJSON(want), test.MustJSON(res))
}

func TestService_updateTargets(t *testing.T) {
	tar := &shard.UpdateTargetsRequest{

		Targets: map[string][]*target.Target{
			"job1": {
				{
					Series: 100,
					Hash:   1,
				},
			},
		},
	}

	called := false
	r := require.New(t)
	a := NewService("", "", os.TempDir(), nil, []func(map[string][]*target.Target) error{
		func(targets map[string][]*target.Target) error {
			called = true
			r.JSONEq(test.MustJSON(tar.Targets), test.MustJSON(targets))
			return nil
		},
	}, nil, nil, logrus.New())

	api.TestCall(t, a.ginEngine.ServeHTTP, "/api/v1/shard/targets/", http.MethodPost, test.MustJSON(tar), nil)

	r.True(called)
	res := &shard.UpdateTargetsRequest{}
	data, err := ioutil.ReadFile(a.storePath())
	r.NoError(err)
	r.NoError(json.Unmarshal(data, res))
	r.JSONEq(test.MustJSON(tar), test.MustJSON(res))
}
