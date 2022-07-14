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

package explore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	scrape2 "github.com/prometheus/prometheus/scrape"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/target"
)

func TestExplore_UpdateTargets(t *testing.T) {
	e := New(scrape.New(true, logrus.New()), prometheus.NewRegistry(), logrus.New())
	require.Nil(t, e.Get(1))
	e.UpdateTargets(map[string][]*discovery.SDTargets{
		"job1": {&discovery.SDTargets{
			ShardTarget: &target.Target{
				Hash:   1,
				Series: 100,
			},
		}},
	})
	r := e.Get(1)
	require.NotNil(t, r)
	require.True(t, e.targets[1].exploring)
}

func TestExplore_Run(t *testing.T) {
	r := require.New(t)
	sm := scrape.New(true, logrus.New())
	r.NoError(sm.ApplyConfig(&prom.ConfigInfo{
		RawContent: nil,
		ConfigHash: "",
		Config: &config.Config{
			ScrapeConfigs: []*config.ScrapeConfig{
				{
					JobName:       "job1",
					ScrapeTimeout: model.Duration(time.Second * 3),
				},
			},
		},
	}))

	e := New(sm, prometheus.NewRegistry(), logrus.New())
	e.retryInterval = time.Millisecond * 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		r.NoError(e.Run(ctx, 1))
	}()

	data := ``
	hts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// must failed first time
		if data == "" {
			data = `metrics{} 1`
			w.WriteHeader(502)
			return
		}
		_, _ = w.Write([]byte(data))
	}))
	defer hts.Close()

	targetURL, err := url.Parse(hts.URL)
	r.NoError(err)

	e.UpdateTargets(map[string][]*discovery.SDTargets{
		"job1": {&discovery.SDTargets{
			ShardTarget: &target.Target{
				Hash:   1,
				Series: 100,
				Labels: labels.Labels{
					{
						Name:  model.SchemeLabel,
						Value: targetURL.Scheme,
					},
					{
						Name:  model.AddressLabel,
						Value: targetURL.Host,
					},
				},
			},
		}},
	})

	res := e.Get(1)
	r.Equal(scrape2.HealthUnknown, res.Health)
	time.Sleep(time.Second)
	res = e.Get(1)
	r.NotNil(res)
	r.Equal(scrape2.HealthGood, res.Health)
	r.Equal(int64(1), res.Series)
	r.Equal("", res.LastError)
}

func TestExplore_ApplyConfig(t *testing.T) {
	r := require.New(t)
	e := New(scrape.New(false, logrus.New()), prometheus.NewRegistry(), logrus.New())
	e.UpdateTargets(map[string][]*discovery.SDTargets{
		"job1": {&discovery.SDTargets{
			ShardTarget: &target.Target{
				Hash:   1,
				Series: 100,
			},
		}},
	})
	r.NoError(e.ApplyConfig(&prom.ConfigInfo{Config: &config.Config{}}))
	require.Nil(t, e.Get(1))
}
