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
	"github.com/gin-gonic/gin"
	"github.com/prometheus/prometheus/config"
	"gopkg.in/yaml.v2"
	"tkestack.io/kvass/pkg/api"
)

func ApiReadConfig(ctx *gin.Context, readConfig func() ([]byte, error)) *api.Result {
	data, err := readConfig()
	if err != nil {
		return api.InternalErr(err, "can not read config")
	}

	return api.Data(gin.H{
		"yaml": string(data),
	})
}

func ApiReloadConfig(readConfig func() ([]byte, error), notify chan *config.Config) *api.Result {
	var (
		err  error
		data []byte
	)
	data, err = readConfig()
	if err != nil {
		return api.InternalErr(err, "read config")
	}

	cfg := &config.Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return api.InternalErr(err, "unmarshal config")
	}

	notify <- cfg
	return api.Data(nil)
}
