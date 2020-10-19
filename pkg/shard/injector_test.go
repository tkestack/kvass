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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInjectConfig(t *testing.T) {
	originData := `
global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
- job_name: test
  honor_timestamps: true
  metrics_path: /metrics
  scheme: https
  static_configs:
  - targets:
    - "127.0.0.1:9091"
`
	origin, injected, err := InjectConfig(
		func() (bytes []byte, e error) {
			return []byte(originData), nil
		},

		func(bytes []byte) error {
			return nil
		},
		"http://127.0.0.1:8080",
	)
	require.NoError(t, err)
	require.Empty(t, origin.ScrapeConfigs[0].HTTPClientConfig.ProxyURL)
	require.Equal(t, "http://127.0.0.1:8080", injected.ScrapeConfigs[0].HTTPClientConfig.ProxyURL.String())
	require.Empty(t, origin.ScrapeConfigs[0].Params)
	require.Equal(t, "test", injected.ScrapeConfigs[0].Params.Get(jobNameFormName))
	require.Equal(t, "https", origin.ScrapeConfigs[0].Scheme)
	require.Equal(t, "http", injected.ScrapeConfigs[0].Scheme)
}
