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
	"time"

	v1 "github.com/prometheus/prometheus/web/api/v1"
	"tkestack.io/kvass/pkg/api"
)

// Client define the Http API of one shard
type Client interface {
	// runtimeInfo return the current status of this shard, only return tManager targets if scrapingOnly is true,
	// otherwise ,all target this client discovered will be returned
	RuntimeInfo() (*RuntimeInfo, error)
	// targets is compatible with PrometheusURL /api/v1/targets
	// the origin PrometheusURL's config is injected, so the targets it report must be adjusted by client sidecar
	Targets(state string) (*v1.TargetDiscovery, error)
	// updateRuntimeInfo tells shard about global targets info, include targets other shard tManager
	UpdateRuntimeInfo(r *RuntimeInfo) error
}

type client struct {
	url string
	cli *http.Client
}

// NewClient return an client
func NewClient(url string, timeout time.Duration) Client {
	return &client{
		url: url,
		cli: &http.Client{
			Timeout: timeout,
		},
	}
}

// runtimeInfo return the current status of this shard
func (s *client) RuntimeInfo() (*RuntimeInfo, error) {
	ret := &RuntimeInfo{}
	return ret, api.Get(fmt.Sprintf("%s/api/v1/shard/runtimeinfo", s.url), ret)
}

// updateRuntimeInfo tells shard about global targets info, include targets other shard tManager
func (s *client) UpdateRuntimeInfo(r *RuntimeInfo) error {
	return api.Post(fmt.Sprintf("%s/api/v1/shard/runtimeinfo", s.url), r, nil)
}

// targets is compatible with PrometheusURL /api/v1/targets
// the origin PrometheusURL's config is injected, so the targets it report must be adjusted by client sidecar
func (s *client) Targets(state string) (*v1.TargetDiscovery, error) {
	return nil, nil
}
