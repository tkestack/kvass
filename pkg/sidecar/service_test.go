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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/test"
)

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
			}))
			defer tProm.Close()

			a := NewService("", tProm.URL, func() (int64, error) {
				return int64(0), nil
			}, prom.NewConfigManager(),
				NewTargetsManager("", prometheus.NewRegistry(), logrus.New()),
				prometheus.NewRegistry(), logrus.New())
			a.ginEngine.POST(a.localPath("/test"), func(context *gin.Context) {})

			r, _ := api.TestCall(t, a.ServeHTTP, cs.uri, http.MethodGet, "", nil)
			r.Equal(cs.wantCall, promCalled)
		})
	}
}

func TestService_Run(t *testing.T) {
	s := NewService("", "", nil, nil, nil, prometheus.NewRegistry(), logrus.New())
	r := require.New(t)
	called := false
	s.runHTTP = func(addr string, handler http.Handler) error {
		r.Equal("123", addr)
		called = true
		return nil
	}
	r.NoError(s.Run("123"))
	r.True(called)
}

func TestService_RuntimeInfo(t *testing.T) {
	cases := []struct {
		name               string
		getPromRuntimeInfo func() (int64, error)
		targets            *shard.UpdateTargetsRequest
		configContent      string
		wantAPIResult      *api.Result
	}{
		{
			name: "prometheus head series return err",
			getPromRuntimeInfo: func() (int64, error) {
				return 0, fmt.Errorf("err")
			},
			targets:       &shard.UpdateTargetsRequest{},
			configContent: "global:",
			wantAPIResult: api.InternalErr(fmt.Errorf("err"), "get runtime from prometheus"),
		},
		{
			name: "prometheus head series < total series of all targets",
			getPromRuntimeInfo: func() (int64, error) {
				return 1, nil
			},
			targets: &shard.UpdateTargetsRequest{
				Targets: map[string][]*target.Target{
					"test": {
						{
							Hash:   1,
							Series: 10,
						},
					},
				},
			},
			configContent: `global:
  evaluation_interval: 10s
  scrape_interval: 15s
scrape_configs:
- job_name: "test"
  static_configs:
  - targets:
    - 127.0.0.1:9091`,
			wantAPIResult: api.Data(&shard.RuntimeInfo{
				HeadSeries:  10,
				ConfigHash:  "16727296455050936695",
				IdleStartAt: nil,
			}),
		},
		{
			name: "prometheus head series > total series of all targets",
			getPromRuntimeInfo: func() (int64, error) {
				return 100, nil
			},
			targets: &shard.UpdateTargetsRequest{
				Targets: map[string][]*target.Target{
					"test": {
						{
							Hash:   1,
							Series: 10,
						},
					},
				},
			},
			configContent: `global:
  evaluation_interval: 10s
  scrape_interval: 15s
scrape_configs:
- job_name: "test"
  static_configs:
  - targets:
    - 127.0.0.1:9091`,
			wantAPIResult: api.Data(&shard.RuntimeInfo{
				HeadSeries:  100,
				ConfigHash:  "16727296455050936695",
				IdleStartAt: nil,
			}),
		},
	}
	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			tm := NewTargetsManager(t.TempDir(), prometheus.NewRegistry(), logrus.New())
			r.NoError(tm.UpdateTargets(cs.targets))

			cfg := path.Join(t.TempDir(), "config.yaml")
			r.NoError(ioutil.WriteFile(cfg, []byte(cs.configContent), 0755))
			cfgMa := prom.NewConfigManager()
			r.NoError(cfgMa.ReloadFromFile(cfg))

			s := NewService("", "", cs.getPromRuntimeInfo, cfgMa, tm, prometheus.NewRegistry(), logrus.New())
			res := s.runtimeInfo(nil)
			r.Equal(cs.wantAPIResult.Status, res.Status)
			if res.Status != api.StatusError {
				r.JSONEq(test.MustJSON(cs.wantAPIResult.Data), test.MustJSON(res.Data))
			}
		})
	}
}

func TestService_UpdateTargets(t *testing.T) {
	data := `
{
  "Targets": {
    "test": [
      {
        "Hash": 1,
        "Series":1
      }
    ]
  }
}
`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/shard/targets/", strings.NewReader(data))
	w := httptest.NewRecorder()

	r := require.New(t)
	tm := NewTargetsManager(t.TempDir(), prometheus.NewRegistry(), logrus.New())
	s := NewService("", "", nil, nil, tm, prometheus.NewRegistry(), logrus.New())
	s.ServeHTTP(w, req)
	result := w.Result()
	r.Equal(200, result.StatusCode)
	r.JSONEq(test.MustJSON(TargetsInfo{
		Targets: map[string][]*target.Target{
			"test": {
				{
					Hash:   1,
					Series: 1,
				},
			},
		},
		IdleAt: nil,
		Status: map[uint64]*target.ScrapeStatus{
			1: target.NewScrapeStatus(1),
		},
	}), test.MustJSON(tm.TargetsInfo()))
}

func TestNewService_UpdateConfig(t *testing.T) {
	type caseInfo struct {
		configFile  string
		content     string
		wantErr     bool
		wantUpdated bool
	}

	successCase := func() *caseInfo {
		return &caseInfo{
			configFile: "",
			content: `global:
  evaluation_interval: 10s
  scrape_interval: 15s
scrape_configs:
- job_name: "test"
  static_configs:
  - targets:
    - 127.0.0.1:9091`,
			wantErr:     false,
			wantUpdated: true,
		}
	}

	var cases = []struct {
		desc       string
		updateCase func(c *caseInfo)
	}{
		{
			desc:       "success",
			updateCase: func(c *caseInfo) {},
		},
		{
			desc: "config file not empty, update config from raw data is not allowed",
			updateCase: func(c *caseInfo) {
				c.configFile = "xx"
				c.wantErr = true
				c.wantUpdated = false
			},
		},
		{
			desc: "wrong data format, want err",
			updateCase: func(c *caseInfo) {
				c.content = `a : a :`
				c.wantErr = true
				c.wantUpdated = false
			},
		},
	}

	for _, cs := range cases {
		t.Run(cs.desc, func(t *testing.T) {
			c := successCase()
			cs.updateCase(c)
			cm := prom.NewConfigManager()
			updated := false
			cm.AddReloadCallbacks(func(c *prom.ConfigInfo) error {
				updated = true
				return nil
			})

			svc := NewService(c.configFile, "", nil, cm, nil, prometheus.NewRegistry(), logrus.New())
			req := &shard.UpdateConfigRequest{
				RawContent: c.content,
			}
			data, _ := json.Marshal(req)
			r, ret := api.TestCall(t, svc.ginEngine.ServeHTTP, "/api/v1/status/config/", http.MethodPost, string(data), nil)
			r.Equal(c.wantUpdated, updated)
			r.Equal(c.wantErr, ret.Status == api.StatusError)
		})
	}
}

func TestNewService_getJobSamples(t *testing.T) {
	type caseInfo struct {
		uri           string
		targetManager *TargetsManager
		wantResutl    map[string]*scrape.StatisticsSeriesResult
	}

	successCase := func() *caseInfo {
		tm := NewTargetsManager(t.TempDir(), prometheus.NewRegistry(), logrus.New())
		tm.UpdateTargets(&shard.UpdateTargetsRequest{
			Targets: map[string][]*target.Target{
				"a": {
					{
						Hash: 1,
					},
					{
						Hash: 2,
					},
				},
			},
		})

		tm.TargetsInfo().Status[1] = &target.ScrapeStatus{
			LastScrapeStatistics: &scrape.StatisticsSeriesResult{
				ScrapedTotal: 1,
				MetricsTotal: map[string]*scrape.MetricSamplesInfo{
					"test": {
						Total:   2,
						Scraped: 1,
					},
				},
			},
		}

		tm.TargetsInfo().Status[2] = &target.ScrapeStatus{
			LastScrapeStatistics: &scrape.StatisticsSeriesResult{
				ScrapedTotal: 1,
				MetricsTotal: map[string]*scrape.MetricSamplesInfo{
					"test": {
						Total:   2,
						Scraped: 1,
					},
					"test2": {
						Total:   1,
						Scraped: 0,
					},
				},
			},
		}

		return &caseInfo{
			uri:           "/api/v1/shard/samples/",
			targetManager: tm,
			wantResutl: map[string]*scrape.StatisticsSeriesResult{
				"a": {
					ScrapedTotal: 2,
					MetricsTotal: map[string]*scrape.MetricSamplesInfo{},
				},
			},
		}
	}
	var cases = []struct {
		desc       string
		updateCase func(c *caseInfo)
	}{
		{
			desc:       "no job filter, witout metrics detail",
			updateCase: func(c *caseInfo) {},
		},
		{
			desc: "no job filter, with metrics detail",
			updateCase: func(c *caseInfo) {
				c.uri = "/api/v1/shard/samples/?with_metrics_detail=true"
				c.wantResutl = map[string]*scrape.StatisticsSeriesResult{
					"a": {
						ScrapedTotal: 2,
						MetricsTotal: map[string]*scrape.MetricSamplesInfo{
							"test": {
								Total:   4,
								Scraped: 2,
							},
							"test2": {
								Total:   1,
								Scraped: 0,
							},
						},
					},
				}
			},
		},
		{
			desc: "with job filter, without metrics detail",
			updateCase: func(c *caseInfo) {
				c.uri = "/api/v1/shard/samples/?job=xxx"
				c.wantResutl = make(map[string]*scrape.StatisticsSeriesResult)
			},
		},
	}
	for _, cs := range cases {
		t.Run(cs.desc, func(t *testing.T) {
			c := successCase()
			cs.updateCase(c)

			svc := NewService("", "", nil, nil, c.targetManager, prometheus.NewRegistry(), logrus.New())
			resp := map[string]*scrape.StatisticsSeriesResult{}
			r, _ := api.TestCall(t, svc.ginEngine.ServeHTTP, c.uri, http.MethodGet, "", &resp)

			r.Equal(c.wantResutl, resp)
		})
	}
}
