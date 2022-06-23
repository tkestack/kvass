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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	scrape2 "github.com/prometheus/prometheus/scrape"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/target"
)

func TestProxy_ServeHTTP(t *testing.T) {
	var cases = []struct {
		name             string
		job              *config.ScrapeConfig
		status           map[uint64]*target.ScrapeStatus
		uri              string
		data             string
		wantStatusCode   int
		wantTargetStatus map[uint64]*target.ScrapeStatus
	}{
		{
			name:             "job not found",
			job:              nil,
			status:           map[uint64]*target.ScrapeStatus{},
			uri:              "/metrics?_jobName=job1&_scheme=http&_hash=1",
			wantStatusCode:   http.StatusBadRequest,
			wantTargetStatus: map[uint64]*target.ScrapeStatus{},
		},
		{
			name: "invalid hash",
			job: &config.ScrapeConfig{
				JobName:       "job1",
				ScrapeTimeout: model.Duration(time.Second * 3),
			},
			status:           map[uint64]*target.ScrapeStatus{},
			uri:              "/metrics?_jobName=job1&_scheme=http&_hash=xxxx",
			wantStatusCode:   http.StatusBadRequest,
			wantTargetStatus: map[uint64]*target.ScrapeStatus{},
		},
		{
			name: "scrape failed",
			job: &config.ScrapeConfig{
				JobName:       "job1",
				ScrapeTimeout: model.Duration(time.Second * 3),
			},
			status: map[uint64]*target.ScrapeStatus{
				1: {},
			},
			uri:            "/metrics?_jobName=job1&_scheme=http&_hash=1",
			data:           ``,
			wantStatusCode: http.StatusBadRequest,
			wantTargetStatus: map[uint64]*target.ScrapeStatus{
				1: {
					Health:      scrape2.HealthBad,
					Series:      0,
					TargetState: target.StateInTransfer,
				},
			},
		},
		{
			name: "scrape success",
			job: &config.ScrapeConfig{
				JobName:       "job1",
				ScrapeTimeout: model.Duration(time.Second * 3),
			},
			status: map[uint64]*target.ScrapeStatus{
				1: {},
			},
			uri:            "/metrics?_jobName=job1&_scheme=http&_hash=1",
			data:           `metrics0{} 1`,
			wantStatusCode: http.StatusOK,
			wantTargetStatus: map[uint64]*target.ScrapeStatus{
				1: {
					Health: scrape2.HealthGood,
					Series: 1,
				},
			},
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if cs.data == "" {
					w.WriteHeader(http.StatusBadRequest)
				} else {
					_, _ = w.Write([]byte(cs.data))
				}
			}))
			defer targetServer.Close()

			p := NewProxy(
				func(jobName string) *scrape.JobInfo {
					if cs.job == nil {
						return nil
					}
					return &scrape.JobInfo{
						Config: cs.job,
						Cli:    http.DefaultClient,
					}
				},
				func() map[uint64]*target.ScrapeStatus {
					return cs.status
				},
				func() *prom.ConfigInfo {
					return prom.DefaultConfig
				},
				prometheus.NewRegistry(),
				logrus.New())

			req := httptest.NewRequest(http.MethodGet, targetServer.URL+cs.uri, strings.NewReader(""))
			w := httptest.NewRecorder()
			p.ServeHTTP(w, req)

			result := w.Result()
			r.Equal(cs.wantStatusCode, result.StatusCode)
			if cs.data != `` {
				d, err := ioutil.ReadAll(result.Body)
				r.NoError(err)
				r.Equal(string(d), cs.data)
			}

			if len(cs.wantTargetStatus) != 0 {
				r.Equal(cs.wantTargetStatus[1].Series, cs.status[1].Series)
				r.Equal(cs.wantTargetStatus[1].Health, cs.status[1].Health)
			}
		})
	}
}
