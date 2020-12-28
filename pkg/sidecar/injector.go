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
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/pkg/relabel"
	"io/ioutil"
	"net/url"
	"os"
	"strings"
	"sync"
	"tkestack.io/kvass/pkg/target"

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
	// ProxyURL will be inject to all job if it is not empty
	ProxyURL string
}

// Injector gen injected config file
type Injector struct {
	sync.Mutex
	originFile     string
	outFile        string
	option         InjectConfigOptions
	curTargets     map[string][]*target.Target
	readFile       func(file string) ([]byte, error)
	writeFile      func(filename string, data []byte, perm os.FileMode) error
	remoteWriteURL string
	remoteReadURL  string
	log            logrus.FieldLogger
}

// NewInjector create new injector with InjectConfigOptions
func NewInjector(originFile, outFile string, option InjectConfigOptions,
	remoteWriteURL, remoteReadURL string,
	log logrus.FieldLogger) *Injector {
	return &Injector{
		originFile:     originFile,
		outFile:        outFile,
		option:         option,
		curTargets:     map[string][]*target.Target{},
		readFile:       ioutil.ReadFile,
		writeFile:      ioutil.WriteFile,
		remoteWriteURL: remoteWriteURL,
		remoteReadURL:  remoteReadURL,
		log:            log,
	}
}

// UpdateTargets set new targets
func (i *Injector) UpdateTargets(ts map[string][]*target.Target) error {
	i.curTargets = ts
	return i.UpdateConfig()
}

// UpdateConfig gen new config
func (i *Injector) UpdateConfig() error {
	i.Lock()
	defer i.Unlock()

	cfgData, err := i.readFile(i.originFile)
	if err != nil {
		return errors.Wrap(err, "read origin file failed")
	}

	cfg := &config.Config{}
	if err := yaml.Unmarshal(cfgData, &cfg); err != nil {
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

	if i.remoteWriteURL != "" {
		cfg.RemoteWriteConfigs = make([]*config.RemoteWriteConfig, 0)
		u, err := url.Parse(i.remoteWriteURL)
		if err != nil {
			return err
		}
		wc := &config.RemoteWriteConfig{URL: &config_util.URL{
			URL: u,
		}}
		cfg.RemoteWriteConfigs = append(cfg.RemoteWriteConfigs, wc)
	}

	if i.remoteReadURL != "" {
		cfg.RemoteReadConfigs = make([]*config.RemoteReadConfig, 0)
		u, err := url.Parse(i.remoteReadURL)
		if err != nil {
			return err
		}
		rc := &config.RemoteReadConfig{URL: &config_util.URL{
			URL: u,
		}}
		cfg.RemoteReadConfigs = append(cfg.RemoteReadConfigs, rc)
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
