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
	"fmt"
	"github.com/prometheus/prometheus/discovery"
	"io/ioutil"
	"net/url"
	"os"
	"strings"
	"sync"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/target"

	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"

	"github.com/prometheus/prometheus/config"
	"github.com/sirupsen/logrus"

	"github.com/pkg/errors"
	config_util "github.com/prometheus/common/config"
	"gopkg.in/yaml.v2"
)

const (
	paramJobName = "_jobName"
	paramHash    = "_hash"
	paramScheme  = "_scheme"
)

// InjectConfigOptions indicate what to inject to config file
type InjectConfigOptions struct {
	// ProxyURL will be injected to all job if it is not empty
	ProxyURL string
	// PrometheusURL will be injected
	PrometheusURL string
}

// Injector gen injected config file
type Injector struct {
	sync.Mutex
	outFile    string
	option     InjectConfigOptions
	curTargets map[string][]*target.Target
	curCfg     *prom.ConfigInfo
	writeFile  func(filename string, data []byte, perm os.FileMode) error
	log        logrus.FieldLogger
}

// NewInjector create new injector with InjectConfigOptions
func NewInjector(outFile string, option InjectConfigOptions, log logrus.FieldLogger) *Injector {
	return &Injector{
		outFile:    outFile,
		option:     option,
		curTargets: map[string][]*target.Target{},
		writeFile:  ioutil.WriteFile,
		log:        log,
	}
}

// UpdateTargets set new targets
func (i *Injector) UpdateTargets(ts map[string][]*target.Target) error {
	i.curTargets = ts
	return i.inject()
}

// ApplyConfig gen new config
func (i *Injector) ApplyConfig(cfg *prom.ConfigInfo) error {
	i.curCfg = cfg
	return i.inject()
}

func (i *Injector) inject() error {
	i.Lock()
	defer i.Unlock()

	cfg := &config.Config{}
	if err := yaml.Unmarshal(i.curCfg.RawContent, &cfg); err != nil {
		return err
	}

	bTokens := make([]string, 0)
	password := make([]string, 0)

	for _, job := range cfg.ScrapeConfigs {
		if i.option.ProxyURL != "" {
			u, err := url.Parse(i.option.ProxyURL)
			if err != nil {
				return err
			}

			job.HTTPClientConfig.ProxyURL = config_util.URL{
				URL: u,
			}
		}

		job.ServiceDiscoveryConfigs = []discovery.Config{
			discovery.StaticConfig(target2targetGroup(job.JobName, i.curTargets[job.JobName])),
		}

		job.Scheme = "http"
		job.HTTPClientConfig.BearerToken = ""
		job.HTTPClientConfig.BasicAuth = nil
		job.HTTPClientConfig.TLSConfig = config_util.TLSConfig{}

		// fix invalid label
		job.RelabelConfigs = []*relabel.Config{
			{
				Separator:   ";",
				Regex:       relabel.MustNewRegexp(target.PrefixForInvalidLabelName + "(.+)"),
				Replacement: "$1",
				Action:      relabel.LabelMap,
			},
		}
	}

	for _, w := range cfg.RemoteWriteConfigs {
		if w.HTTPClientConfig.BearerToken != "" {
			bTokens = append(bTokens, string(w.HTTPClientConfig.BearerToken))
		}

		if w.HTTPClientConfig.BasicAuth != nil && w.HTTPClientConfig.BasicAuth.Password != "" {
			password = append(password, string(w.HTTPClientConfig.BasicAuth.Password))
		}

	}

	for _, w := range cfg.RemoteReadConfigs {
		if w.HTTPClientConfig.BearerToken != "" {
			bTokens = append(bTokens, string(w.HTTPClientConfig.BearerToken))
		}

		if w.HTTPClientConfig.BasicAuth != nil && w.HTTPClientConfig.BasicAuth.Password != "" {
			password = append(password, string(w.HTTPClientConfig.BasicAuth.Password))
		}
	}
	if i.option.PrometheusURL != "" {
		u, _ := url.Parse(i.option.PrometheusURL)
		podName := os.Getenv("POD_NAME")
		ss := strings.Split(podName, "-")
		shard := "0"
		if len(ss) > 0 {
			shard = ss[len(ss)-1]
		}

		cfg.ScrapeConfigs = append(cfg.ScrapeConfigs, &config.ScrapeConfig{
			JobName: "prometheus_shards",
			ServiceDiscoveryConfigs: []discovery.Config{
				discovery.StaticConfig([]*targetgroup.Group{
					{
						Targets: []model.LabelSet{
							{
								model.AddressLabel: model.LabelValue(u.Host),
							},
						},
						Labels: map[model.LabelName]model.LabelValue{
							"replicate": model.LabelValue(podName),
							"shard":     model.LabelValue(shard),
						},
					},
				}),
			}})
	}

	gen, err := yaml.Marshal(&cfg)
	if err != nil {
		return errors.Wrapf(err, "marshal config failed")
	}

	data := string(gen)
	for _, token := range bTokens {
		data = strings.Replace(data, "bearer_token: <secret>", fmt.Sprintf("bearer_token: %s", token), 1)
	}

	for _, pd := range password {
		data = strings.Replace(data, "password: <secret>", fmt.Sprintf("password: %s", pd), 1)
	}

	if err := i.writeFile(i.outFile, []byte(data), 0755); err != nil {
		return errors.Wrapf(err, "write file failed")
	}

	i.log.Infof("config inject completed")
	return nil
}

func target2targetGroup(job string, ts []*target.Target) []*targetgroup.Group {
	ret := make([]*targetgroup.Group, 0)

	for _, t := range ts {
		ls := model.LabelSet{}
		scheme := "http"
		address := ""
		for _, v := range t.Labels {
			if v.Name == model.SchemeLabel {
				scheme = v.Value
			}
			if v.Name == model.AddressLabel {
				address = v.Value
			}

			ls[model.LabelName(v.Name)] = model.LabelValue(v.Value)
		}

		ls[model.LabelName(model.SchemeLabel)] = "http"
		ls[model.LabelName(fmt.Sprintf("%s%s", model.ParamLabelPrefix, paramScheme))] = model.LabelValue(scheme)
		ls[model.LabelName(fmt.Sprintf("%s%s", model.ParamLabelPrefix, paramJobName))] = model.LabelValue(job)
		ls[model.LabelName(fmt.Sprintf("%s%s", model.ParamLabelPrefix, paramHash))] = model.LabelValue(fmt.Sprint(t.Hash))

		ret = append(ret, &targetgroup.Group{
			Targets: []model.LabelSet{
				{
					model.AddressLabel: model.LabelValue(address),
				},
			},
			Labels: ls,
		})
	}

	return ret
}
