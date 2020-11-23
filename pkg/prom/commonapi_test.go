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
	"net/http"
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
	e := gin.Default()
	c := make(chan *config.Config, 10)
	e.POST("/-/reload", api.Wrap(logrus.New(), func(ctx *gin.Context) *api.Result {
		return APIReloadConfig(func() (bytes []byte, e error) {
			return []byte(configTest), nil
		}, c)
	}))

	_ = api.TestCall(t, e.ServeHTTP, "/-/reload", http.MethodPost, "", nil)
	select {
	case <-c:
		return
	default:
		t.Fatalf("config reload not triggered")
	}
}

func TestConfig(t *testing.T) {
	e := gin.Default()
	e.GET("/api/v1/status/config", api.Wrap(logrus.New(), func(ctx *gin.Context) *api.Result {
		return APIReadConfig(func() (bytes []byte, e error) {
			return []byte(configTest), nil
		})
	}))
	ret := map[string]string{}
	r := api.TestCall(t, e.ServeHTTP, "/api/v1/status/config", http.MethodGet, "", &ret)
	r.Equal(configTest, ret["yaml"])
}
