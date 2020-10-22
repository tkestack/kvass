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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"tkestack.io/kvass/pkg/prom"

	"github.com/sirupsen/logrus"

	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/config"
)

func TestProxy_ServeHTTP(t *testing.T) {
	jobName := "test"
	url := "http://127.0.0.1/metrics"
	var cases = []struct {
		name           string
		scrapeInfo     *prom.ScrapeInfo
		target         *Target
		metricsData    string
		wantData       string
		wantStatusCode int
		wantSamples    int64
	}{
		{
			name: "scrapeInfo not found",
			scrapeInfo: &prom.ScrapeInfo{
				Config: &config.ScrapeConfig{JobName: "null", Scheme: "http"},
			},
			target:         &Target{JobName: jobName, URL: url},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "target not found",
			scrapeInfo: &prom.ScrapeInfo{
				Config: &config.ScrapeConfig{JobName: jobName, Scheme: "http"},
			},
			target:         &Target{JobName: "null", URL: "null"},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "target not scraping, target is up",
			scrapeInfo: &prom.ScrapeInfo{
				Config: &config.ScrapeConfig{JobName: jobName, Scheme: "http"},
			},
			target: &Target{
				JobName: jobName,
				URL:     url,
				Healthy: "up",
			},
			metricsData:    "metrics0{} 1",
			wantData:       "",
			wantStatusCode: 200,
		},
		{
			name: "target not scraping, target is down",
			scrapeInfo: &prom.ScrapeInfo{
				Config: &config.ScrapeConfig{JobName: jobName, Scheme: "http"},
			},
			target: &Target{
				JobName: jobName,
				URL:     url,
				Healthy: "down",
			},
			metricsData:    "metrics0{} 1",
			wantData:       "",
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "target scraping",
			scrapeInfo: &prom.ScrapeInfo{
				Config: &config.ScrapeConfig{JobName: jobName, Scheme: "http"},
			},
			target: &Target{
				JobName:  jobName,
				URL:      url,
				Healthy:  "up",
				scraping: true,
			},
			metricsData:    "metrics0{} 1",
			wantData:       "metrics0{} 1",
			wantStatusCode: 200,
			wantSamples:    1,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			metricsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				r.Empty(req.Form.Get(jobNameFormName))
				w.Write([]byte(cs.metricsData))
			}))
			defer metricsServer.Close()

			cs.target.URL = metricsServer.URL
			req := httptest.NewRequest("GET", metricsServer.URL+fmt.Sprintf("?%s=%s", jobNameFormName, jobName), nil)
			w := httptest.NewRecorder()

			tm := NewTargetManager(logrus.New())

			shardID := "shard-0"
			if !cs.target.scraping {
				shardID = "shard-1"
			}

			tm.Update(map[string][]*Target{
				shardID: {cs.target},
			}, "shard-0")

			p := NewProxy(tm, logrus.New())
			cs.scrapeInfo.Client = metricsServer.Client()
			p.ScrapeInfos = prom.NewScrapInfos(map[string]*prom.ScrapeInfo{
				cs.scrapeInfo.Config.JobName: cs.scrapeInfo,
			})

			p.ServeHTTP(w, req)

			result := w.Result()
			defer result.Body.Close()

			r.Equal(cs.wantStatusCode, result.StatusCode)
			if len(cs.wantData) != 0 {
				data, err := ioutil.ReadAll(result.Body)
				r.NoError(err)
				r.Equal(cs.wantData, string(data))
			}

			if cs.wantSamples != 0 {
				r.Equal(cs.wantSamples, tm.Get(jobName, metricsServer.URL).Samples)
			}
		})
	}
}
