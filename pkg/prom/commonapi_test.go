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
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"net/http"
	"path"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/prometheus/config"

	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/api"
)

const configTest = `
global:
  scrape_interval:     15s # Set the scrape interval to every 15 seconds. Default is every 1 minute.
  evaluation_interval: 15s # Evaluate rules every 15 seconds. The default is every 1 minute.
`

func TestApiReloadConfig(t *testing.T) {
	r := require.New(t)
	e := gin.Default()
	file := path.Join(t.TempDir(), "config.yaml")
	r.NoError(ioutil.WriteFile(file, []byte(configTest), 0777))

	reload := false
	apply := []func(cfg *config.Config) error{
		func(cfg *config.Config) error {
			reload = true
			r.NotNil(cfg)
			return nil
		},
	}

	e.POST("/-/reload", api.Wrap(logrus.New(), func(ctx *gin.Context) *api.Result {
		return APIReloadConfig(logrus.New(), file, apply)
	}))

	_ = api.TestCall(t, e.ServeHTTP, "/-/reload", http.MethodPost, "", nil)
	r.True(reload)
}

func TestConfig(t *testing.T) {
	r := require.New(t)
	e := gin.Default()
	file := path.Join(t.TempDir(), "config.yaml")
	r.NoError(ioutil.WriteFile(file, []byte(configTest), 0777))
	e.GET("/api/v1/status/config", api.Wrap(logrus.New(), func(ctx *gin.Context) *api.Result {
		return APIReadConfig(file)
	}))
	ret := map[string]string{}
	_ = api.TestCall(t, e.ServeHTTP, "/api/v1/status/config", http.MethodGet, "", &ret)
	r.Equal(configTest, ret["yaml"])
}
