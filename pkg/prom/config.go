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
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/config"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"tkestack.io/kvass/pkg/utils/encode"
)

// ConfigInfo include all information of current config
type ConfigInfo struct {
	// RawContent is the content of config file
	RawContent []byte
	// Md5 is a md5 of config file content
	Md5 string
	// Config is the marshaled prometheus config
	Config *config.Config
}

// ConfigManager do config manager
type ConfigManager struct {
	file          string
	callbacks     []func(cfg *ConfigInfo) error
	currentConfig *ConfigInfo
	log           logrus.FieldLogger
}

// NewConfigManager return an config manager
func NewConfigManager(file string, log logrus.FieldLogger) *ConfigManager {
	return &ConfigManager{
		file: file,
		log:  log,
	}
}

// Reload reload config from file and do all callbacks
func (c *ConfigManager) Reload() error {
	if err := c.updateConfigInfo(); err != nil {
		return err
	}

	for _, f := range c.callbacks {
		if err := f(c.currentConfig); err != nil {
			return err
		}
	}

	return nil
}

func (c *ConfigManager) updateConfigInfo() (err error) {
	info := &ConfigInfo{}
	info.RawContent, err = ioutil.ReadFile(c.file)
	if err != nil {
		return errors.Wrapf(err, "read file")
	}

	if len(info.RawContent) == 0 {
		return errors.Wrapf(err, "config content is empty")
	}

	info.Config, err = config.LoadFile(c.file)
	if err != nil {
		return errors.Wrapf(err, "marshal config")
	}

	info.Md5 = encode.Md5(info.RawContent)
	c.currentConfig = info
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
