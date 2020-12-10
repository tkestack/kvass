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
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	"os"
	"testing"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/test"
)

func TestInjector_UpdateConfig(t *testing.T) {
	job := &config.ScrapeConfig{
		JobName: "job",
	}
	job.HTTPClientConfig.BearerToken = "123"

	cfg := &config.Config{
		ScrapeConfigs: []*config.ScrapeConfig{job},
	}

	tar := &target.Target{
		Hash: 1,
		Labels: labels.Labels{
			{
				Name:  model.AddressLabel,
				Value: "127.0.0.1:80",
			},
			{
				Name:  model.SchemeLabel,
				Value: "https",
			},
			{
				Name:  model.MetricsPathLabel,
				Value: "/metrics",
			},
		},
	}

	r := require.New(t)
	in := NewInjector("", "",
		InjectConfigOptions{ProxyURL: "http://127.0.0.1:8008"}, logrus.New())

	out := &config.Config{}
	in.readFile = func(file string) (bytes []byte, e error) {
		return []byte(test.MustYAMLV2(cfg)), nil
	}
	in.writeFile = func(filename string, data []byte, perm os.FileMode) error {
		return yaml.Unmarshal(data, out)
	}

	r.NoError(in.UpdateTargets(map[string][]*target.Target{
		job.JobName: {tar},
	}))

	outJob := out.ScrapeConfigs[0]
	outSD := outJob.ServiceDiscoveryConfigs[0].(discovery.StaticConfig)[0]
	r.Equal("http://127.0.0.1:8008", outJob.HTTPClientConfig.ProxyURL.String())
	r.Equal(model.LabelValue("127.0.0.1:80"), outSD.Targets[0][model.AddressLabel])
	r.Equal(model.LabelValue("http"), outSD.Labels[model.SchemeLabel])
	r.Equal(model.LabelValue("/metrics"), outSD.Labels[model.MetricsPathLabel])
	r.Equal(model.LabelValue("https"), outSD.Labels[model.ParamLabelPrefix+paramScheme])
	r.Equal(model.LabelValue("job"), outSD.Labels[model.ParamLabelPrefix+paramJobName])
	r.Equal(model.LabelValue("1"), outSD.Labels[model.ParamLabelPrefix+paramHash])
}
