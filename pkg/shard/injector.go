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
	"net/url"

	"github.com/prometheus/prometheus/config"

	"github.com/pkg/errors"
	config_util "github.com/prometheus/common/config"
	"gopkg.in/yaml.v3"
)

const (
	jobNameFormName = "_jobName"
)

// InjectConfig inject following info to prometheus config
// 1. inject Proxy url for every job: set proxy_url to url of "client sidecar"
// 2. inject jobName for every job: the jobName will be saved into "params" as "_jobName" since
//    sidecar need to known the jobName of every TargetManager request
// 3. change all scheme to http
func InjectConfig(readOrigin func() ([]byte, error),
	writeInjected func([]byte) error,
	proxyURL string) (origin *config.Config, injected *config.Config, err error) {
	cfgData, err := readOrigin()
	if err != nil {
		return nil, nil, errors.Wrap(err, "read origin file failed")
	}

	origin = &config.Config{}
	injected = &config.Config{}
	if err := yaml.Unmarshal(cfgData, &origin); err != nil {
		return nil, nil, err
	}

	if err := yaml.Unmarshal(cfgData, &injected); err != nil {
		return nil, nil, err
	}

	for _, job := range injected.ScrapeConfigs {
		if job.Params == nil {
			job.Params = map[string][]string{}
		}
		job.Params[jobNameFormName] = []string{job.JobName}

		u, _ := url.Parse(proxyURL)
		job.HTTPClientConfig.ProxyURL = config_util.URL{
			URL: u,
		}
		job.Scheme = "http"
	}

	gen, err := yaml.Marshal(&injected)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "marshal config failed")
	}

	if err := writeInjected(gen); err != nil {
		return nil, nil, errors.Wrapf(err, "write file failed")
	}

	return origin, injected, nil
}
