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

package prom

import (
	"tkestack.io/kvass/pkg/api"

	v1 "github.com/prometheus/prometheus/web/api/v1"
)

type Client interface {
	// runtimeInfo return the current status of this shard, only return tManager targets if scrapingOnly is true,
	// otherwise ,all target this Client discovered will be returned
	RuntimeInfo() (*RuntimeInfo, error)
	// targets is compatible with PrometheusUrl /api/v1/targets
	// the origin PrometheusUrl's Config is injected, so the targets it report must be adjusted by Client sidecar
	Targets(state string) (*v1.TargetDiscovery, error)
	// ConfigReload do Config reloading
	ConfigReload() error
}

type client struct {
	url string
}

func NewClient(url string) Client {
	return &client{
		url: url,
	}
}

func (c *client) RuntimeInfo() (*RuntimeInfo, error) {
	ret := &RuntimeInfo{}
	return ret, api.Get(c.url+"/api/v1/status/runtimeinfo", ret)
}

func (c *client) Targets(state string) (*v1.TargetDiscovery, error) {
	url := c.url + "/api/v1/targets"
	if state != "" {
		url += "?state=" + state
	}
	ret := &v1.TargetDiscovery{}
	return ret, api.Get(url, ret)
}

func (c *client) ConfigReload() error {
	url := c.url + "/-/reload"
	return api.Post(url, nil, nil)
}
