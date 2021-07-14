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
	"github.com/cssivision/reverseproxy"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"net/http"
	"net/url"
	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/utils/types"
)

// Service is the api server of shard
type Service struct {
	lg            logrus.FieldLogger
	configFile    string
	ginEngine     *gin.Engine
	cfgManager    *prom.ConfigManager
	targetManager *TargetsManager
	promURL       string
	getHeadSeries func() (int64, error)
	paths         []string
	runHTTP       func(addr string, handler http.Handler) error
}

// NewService create new api server of shard
func NewService(
	configFile string,
	promURL string,
	getHeadSeries func() (int64, error),
	cfgManager *prom.ConfigManager,
	targetManager *TargetsManager,
	lg logrus.FieldLogger) *Service {

	s := &Service{
		configFile:    configFile,
		promURL:       promURL,
		ginEngine:     gin.Default(),
		lg:            lg,
		getHeadSeries: getHeadSeries,
		runHTTP:       http.ListenAndServe,
		cfgManager:    cfgManager,
		targetManager: targetManager,
	}

	pprof.Register(s.ginEngine)
	s.ginEngine.GET(s.path("/api/v1/shard/runtimeinfo/"), api.Wrap(s.lg, s.runtimeInfo))
	s.ginEngine.GET(s.path("/api/v1/shard/targets/"), api.Wrap(s.lg, func(ctx *gin.Context) *api.Result {
		return api.Data(s.targetManager.TargetsInfo().Status)
	}))
	s.ginEngine.POST(s.path("/api/v1/shard/targets/"), api.Wrap(s.lg, s.updateTargets))
	s.ginEngine.POST(s.path("/-/reload/"), api.Wrap(lg, func(ctx *gin.Context) *api.Result {
		if err := s.cfgManager.ReloadFromFile(configFile); err != nil {
			return api.BadDataErr(err, "reload failed")
		}
		return api.Data(nil)
	}))
	s.ginEngine.GET("/api/v1/status/config/", api.Wrap(lg, func(ctx *gin.Context) *api.Result {
		return api.Data(gin.H{"yaml": string(s.cfgManager.ConfigInfo().RawContent)})
	}))
	s.ginEngine.POST(s.path("/api/v1/status/config/"), api.Wrap(lg, s.updateConfig))

	return s
}

func (s *Service) path(p string) string {
	s.paths = append(s.paths, p)
	return p
}

func (s *Service) ServeHTTP(wt http.ResponseWriter, r *http.Request) {
	if types.FindStringVague(r.URL.Path, s.paths...) {
		s.ginEngine.ServeHTTP(wt, r)
		return
	}

	u, _ := url.Parse(s.promURL)
	reverseproxy.NewReverseProxy(u).ServeHTTP(wt, r)
}

// Run start Service at "address"
func (s *Service) Run(address string) error {
	return s.runHTTP(address, s)
}

func (s *Service) runtimeInfo(g *gin.Context) *api.Result {
	series, err := s.getHeadSeries()
	if err != nil {
		return api.InternalErr(err, "get runtime from prometheus")
	}

	targets := s.targetManager.TargetsInfo()

	min := int64(0)
	for _, r := range targets.Status {
		min += r.Series
	}

	if series < min {
		series = min
	}
	return api.Data(&shard.RuntimeInfo{
		HeadSeries:  series,
		ConfigHash:  s.cfgManager.ConfigInfo().ConfigHash,
		IdleStartAt: targets.IdleAt,
	})
}

func (s *Service) updateTargets(g *gin.Context) *api.Result {
	r := &shard.UpdateTargetsRequest{}
	if err := g.BindJSON(&r); err != nil {
		return api.BadDataErr(err, "bind json")
	}

	if err := s.targetManager.UpdateTargets(r); err != nil {
		return api.InternalErr(err, "")
	}

	return api.Data(nil)
}

func (s *Service) updateConfig(g *gin.Context) *api.Result {
	if s.configFile != "" {
		s.lg.Warnf("config file is set, raw content config update is not allowed")
		return api.BadDataErr(fmt.Errorf("config file is set, raw content config update is not allowed"), "")
	}

	r := &shard.UpdateConfigRequest{}
	if err := g.BindJSON(&r); err != nil {
		return api.BadDataErr(err, "bind json")
	}

	if err := s.cfgManager.ReloadFromRaw([]byte(r.RawContent)); err != nil {
		return api.BadDataErr(err, "reload failed")
	}

	return api.Data(nil)
}
