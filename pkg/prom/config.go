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
	"fmt"
	"io/ioutil"

	"github.com/go-kit/log"
	"github.com/mitchellh/hashstructure/v2"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
)

const (
	defaultConfig = `
global:
  external_labels:
    status: default
`
)

// ExtraConfig is config about kvass it self , not Prometheus config
type ExtraConfig struct {
	// StopScrapeReason ,if not empty, all scrape will failed
	StopScrapeReason string `json:"stopScrapeReason"`
}

// EQ return true if all ExtraConfig fields is eq
func (c *ExtraConfig) EQ(e *ExtraConfig) bool {
	return c.StopScrapeReason == e.StopScrapeReason
}

// ConfigInfo include all information of current config
type ConfigInfo struct {
	// RawContent is the content of config file
	RawContent []byte
	// ConfigHash is a md5 of config file content
	ConfigHash string
	// Config is the marshaled prometheus config
	Config *config.Config
	// ExtraConfig contains Config not in origin Prometheus define
	ExtraConfig *ExtraConfig
}

// DefaultConfig init a ConfigInfo with default prometheus config
var DefaultConfig = &ConfigInfo{
	RawContent:  []byte(defaultConfig),
	ConfigHash:  "",
	Config:      &config.DefaultConfig,
	ExtraConfig: &ExtraConfig{},
}

// ConfigManager do config manager
type ConfigManager struct {
	callbacks     []func(cfg *ConfigInfo) error
	currentConfig *ConfigInfo
}

// NewConfigManager return an config manager
func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		currentConfig: &ConfigInfo{
			RawContent:  []byte(defaultConfig),
			ConfigHash:  "",
			Config:      &config.DefaultConfig,
			ExtraConfig: &ExtraConfig{},
		},
	}
}

// ReloadFromFile reload config from file and do all callbacks
func (c *ConfigManager) ReloadFromFile(file string) error {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	return c.ReloadFromRaw(data)
}

// ReloadFromRaw reload config from raw data
func (c *ConfigManager) ReloadFromRaw(data []byte) (err error) {
	info := &ConfigInfo{
		ExtraConfig: c.currentConfig.ExtraConfig,
	}
	info.RawContent = data
	if len(info.RawContent) == 0 {
		return errors.New("config content is empty")
	}

	info.Config, err = config.Load(string(data), true, log.NewNopLogger())
	if err != nil {
		return errors.Wrapf(err, "marshal config")
	}

	// config hash don't include external labels
	eLb := info.Config.GlobalConfig.ExternalLabels
	info.Config.GlobalConfig.ExternalLabels = []labels.Label{}
	hash, err := hashstructure.Hash(info.Config, hashstructure.FormatV2, nil)
	if err != nil {
		return errors.Wrapf(err, "get config hash")
	}

	info.ConfigHash = fmt.Sprint(hash)
	info.Config.GlobalConfig.ExternalLabels = eLb
	c.currentConfig = info

	for _, f := range c.callbacks {
		if err := f(c.currentConfig); err != nil {
			return err
		}
	}

	return nil
}

// UpdateExtraConfig set new extra config
func (c *ConfigManager) UpdateExtraConfig(cfg ExtraConfig) error {
	if c.currentConfig.ExtraConfig.EQ(&cfg) {
		return nil
	}

	c.currentConfig.ExtraConfig = &cfg
	for _, f := range c.callbacks {
		if err := f(c.currentConfig); err != nil {
			return err
		}
	}
	return nil
}

// ConfigInfo return current config info
func (c *ConfigManager) ConfigInfo() *ConfigInfo {
	return c.currentConfig
}

// AddReloadCallbacks add callbacks of config reload event
func (c *ConfigManager) AddReloadCallbacks(f ...func(c *ConfigInfo) error) {
	c.callbacks = append(c.callbacks, f...)
}
