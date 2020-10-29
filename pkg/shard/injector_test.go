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
  bearer_token: "123"
  scheme: https
  static_configs:
  - targets:
    - "127.0.0.1:9091"
  kubernetes_sd_configs:
  - role: endpoints
`
	i := NewInjector("", "", "")
	i.readFile = func(name string) (bytes []byte, e error) {
		return []byte(originData), nil
	}

	i.writeFile = func(filename string, data []byte) error {
		return nil
	}

	origin, injected, err := i.InjectConfig(
		InjectConfigOptions{
			ProxyURL: "http://127.0.0.1:8080",
			KubernetesSD: InjectConfigKubernetesSD{
				APIServerURL: "https://127.0.0.1:443",
				Token:        "token-test",
				ProxyURL:     "http://127.0.0.1:8009",
			},
		},
	)

	require.NoError(t, err)
	iCfg := injected.ScrapeConfigs[0]
	require.Empty(t, origin.ScrapeConfigs[0].HTTPClientConfig.ProxyURL)
	require.Equal(t, "http://127.0.0.1:8080", iCfg.HTTPClientConfig.ProxyURL.String())
	require.Empty(t, origin.ScrapeConfigs[0].Params)
	require.Equal(t, "test", iCfg.Params.Get(jobNameFormName))
	require.NotEmpty(t, iCfg.HTTPClientConfig.BearerTokenFile)

	require.Equal(t, "https", origin.ScrapeConfigs[0].Scheme)
	require.Equal(t, "http", iCfg.Scheme)
	require.Equal(t, "https://127.0.0.1:443", iCfg.ServiceDiscoveryConfig.KubernetesSDConfigs[0].APIServer.String())
	require.Equal(t, "http://127.0.0.1:8009", iCfg.ServiceDiscoveryConfig.KubernetesSDConfigs[0].HTTPClientConfig.ProxyURL.String())
}
