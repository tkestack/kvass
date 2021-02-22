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
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/scrape"
	v1 "github.com/prometheus/prometheus/web/api/v1"
	"github.com/sirupsen/logrus"
	"net/http"
	"net/url"
	"testing"
	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/target"
)

func TestAPI_Targets(t *testing.T) {
	getScrapeStatus := func() map[uint64]*target.ScrapeStatus {
		return map[uint64]*target.ScrapeStatus{
			1: {
				Health:    scrape.HealthBad,
				LastError: "test",
			},
		}
	}

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

	getActive := func() map[string][]*discovery.SDTargets {
		return map[string][]*discovery.SDTargets{
			"j1": {
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
	getDrop := func() map[string][]*discovery.SDTargets {
		return map[string][]*discovery.SDTargets{
			"j1": {
				{
					PromTarget: scrape.NewTarget(lbs, lbs, url.Values{}),
				},
			},
		}
	}

	a := NewService(prom.NewConfigManager("", logrus.New()), getScrapeStatus, getActive, getDrop, logrus.New())
	res := &v1.TargetDiscovery{}
	r := api.TestCall(t, a.Engine.ServeHTTP, "/api/v1/targets", http.MethodGet, "", res)
	r.Equal("http://127.0.0.1:80", res.ActiveTargets[0].ScrapeURL)
	r.Equal("j1", res.ActiveTargets[0].ScrapePool)
	r.Equal(scrape.HealthBad, res.ActiveTargets[0].Health)
	r.Equal("test", res.ActiveTargets[0].LastError)
	r.NotEmpty(res.DroppedTargets)
}
