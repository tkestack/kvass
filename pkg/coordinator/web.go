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

package coordinator

import (
	"tkestack.io/kvass/pkg/prom"

	"tkestack.io/kvass/pkg/api"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/prometheus/config"
	"github.com/sirupsen/logrus"
)

// Web is the api server of coordinator
type Web struct {
	// gin.Engine is the gin engine for handle http request
	*gin.Engine
	ConfigReload chan *config.Config
	lg           logrus.FieldLogger
	readConfig   func() ([]byte, error)
}

// NewWeb return a new web server
func NewWeb(
	readConfig func() ([]byte, error),
	lg logrus.FieldLogger) *Web {
	w := &Web{
		ConfigReload: make(chan *config.Config, 2),
		Engine:       gin.Default(),
		lg:           lg,
		readConfig:   readConfig,
	}

	w.GET("/api/v1/status/config", api.Wrap(lg, func(ctx *gin.Context) *api.Result {
		return prom.APIReadConfig(ctx, readConfig)
	}))
	w.GET("/api/v1/targets", api.Wrap(lg, w.targets))
	w.POST("/-/reload", api.Wrap(lg, func(ctx *gin.Context) *api.Result {
		return prom.APIReloadConfig(readConfig, w.ConfigReload)
	}))
	return w
}

func (w *Web) targets(ctx *gin.Context) *api.Result {
	return nil
}
