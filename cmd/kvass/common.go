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

package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/url"
	"os"
	"tkestack.io/kvass/pkg/target"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/config"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

func initConfig(file string) *config.Config {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		panic(err)
	}

	m := &config.Config{}
	if err := yaml.Unmarshal(data, m); err != nil {
		panic(err.Error())
	}
	return m
}

func initTargets(file string) map[string][]*target.Target {
	data, err := ioutil.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}

	m := map[string][]*target.Target{}
	if err == nil {
		if err := json.Unmarshal(data, &m); err != nil {
			panic(err)
		}
	}

	return m
}

type configInjectOption struct {
	kubernetes struct {
		url   string
		token string
		proxy string
	}
}

func reloadConfig(ctx context.Context, option *configInjectOption, configReload chan *config.Config, lg logrus.FieldLogger, applyConfigs []func(cfg *config.Config) error) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case cfg := <-configReload:
			configInject(cfg, option)
			for _, f := range applyConfigs {
				if err := f(cfg); err != nil {
					lg.Error(err.Error())
				}
			}
		}
	}
}

func configInject(cfg *config.Config, option *configInjectOption) {
	if option == nil {
		return
	}
	for _, job := range cfg.ScrapeConfigs {
		if option.kubernetes.url != "" && job.ServiceDiscoveryConfig.KubernetesSDConfigs != nil {
			for _, sd := range job.ServiceDiscoveryConfig.KubernetesSDConfigs {
				u, _ := url.Parse(option.kubernetes.url)
				sd.APIServer = config_util.URL{URL: u}
				sd.HTTPClientConfig.BearerToken = config_util.Secret(option.kubernetes.token)
				if option.kubernetes.proxy != "" {
					u, _ := url.Parse(option.kubernetes.proxy)
					sd.HTTPClientConfig.ProxyURL = config_util.URL{URL: u}
				}
				sd.HTTPClientConfig.TLSConfig.InsecureSkipVerify = true
			}
		}
	}
}

func reloadTargets(ctx context.Context, targetsReload chan map[string][]*target.Target, lg logrus.FieldLogger,
	applyConfigs []func(map[string][]*target.Target) error) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case cfg := <-targetsReload:
			for _, f := range applyConfigs {
				if err := f(cfg); err != nil {
					lg.Error(err.Error())
				}
			}
		}
	}
}
