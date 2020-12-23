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
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/config"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/types"
)

// Service is the api server of shard
type Service struct {
	lg                 logrus.FieldLogger
	ginEngine          *gin.Engine
	promURL            string
	configFile         string
	storeDir           string
	applyTargets       []func(map[string][]*target.Target) error
	applyConfigs       []func(*config.Config) error
	getPromRuntimeInfo func() (*prom.RuntimeInfo, error)
	getTargetStatus    func() map[uint64]*target.ScrapeStatus
	lastReBalanceAt    time.Time
	paths              []string
	runHTTP            func(addr string, handler http.Handler) error
}

// NewService create new api server of shard
func NewService(
	promURL string,
	configFile string,
	storeDir string,
	applyConfigs []func(*config.Config) error,
	applyTargets []func(map[string][]*target.Target) error,
	getPromRuntimeInfo func() (*prom.RuntimeInfo, error),
	getTargetStatus func() map[uint64]*target.ScrapeStatus,
	lg logrus.FieldLogger) *Service {

	s := &Service{
		ginEngine:          gin.Default(),
		lg:                 lg,
		promURL:            promURL,
		storeDir:           storeDir,
		configFile:         configFile,
		getPromRuntimeInfo: getPromRuntimeInfo,
		getTargetStatus:    getTargetStatus,
		applyTargets:       applyTargets,
		applyConfigs:       applyConfigs,
		runHTTP:            http.ListenAndServe,
	}

	pprof.Register(s.ginEngine)
	s.ginEngine.GET(s.path("/api/v1/shard/runtimeinfo/"), api.Wrap(s.lg, s.runtimeInfo))
	s.ginEngine.GET(s.path("/api/v1/shard/targets/"), api.Wrap(s.lg, s.getTargets))
	s.ginEngine.POST(s.path("/api/v1/shard/targets/"), api.Wrap(s.lg, s.updateTargets))
	s.ginEngine.GET(s.path("/api/v1/status/config/"), api.Wrap(lg, func(ctx *gin.Context) *api.Result {
		return prom.APIReadConfig(configFile)
	}))
	s.ginEngine.POST(s.path("/-/reload/"), api.Wrap(lg, func(ctx *gin.Context) *api.Result {
		return prom.APIReloadConfig(s.lg, configFile, applyConfigs)
	}))
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
	httputil.NewSingleHostReverseProxy(u).ServeHTTP(wt, r)
}

// Init do config init and load targets from disk
func (s *Service) Init() error {
	if err := prom.APIReloadConfig(s.lg, s.configFile, s.applyConfigs).Err; err != "" {
		return fmt.Errorf(err)
	}

	rt, err := s.loadTargets()
	if err != nil {
		return errors.Wrapf(err, "load targets")
	}

	if err := s.doApplyTargets(rt); err != nil {
		return errors.Wrapf(err, "apply targets")
	}

	return nil
}

// Run start Service at "address"
func (s *Service) Run(address string) error {
	return s.runHTTP(address, s)
}

func (s *Service) runtimeInfo(g *gin.Context) *api.Result {
	r, err := s.getPromRuntimeInfo()
	if err != nil {
		return api.InternalErr(err, "get runtime from prometheus")
	}

	min := int64(0)
	for _, r := range s.getTargetStatus() {
		min += r.Series
	}

	if r.TimeSeriesCount > min {
		r.TimeSeriesCount = min
	}
	return api.Data(&shard.RuntimeInfo{
		HeadSeries: r.TimeSeriesCount,
	})
}

func (s *Service) getTargets(g *gin.Context) *api.Result {
	return api.Data(s.getTargetStatus())
}

func (s *Service) updateTargets(g *gin.Context) *api.Result {
	r := &shard.UpdateTargetsRequest{}
	if err := g.BindJSON(&r); err != nil {
		return api.BadDataErr(err, "bind json")
	}

	if err := s.doApplyTargets(r); err != nil {
		return api.InternalErr(err, "apply updating targets")
	}

	data, _ := json.Marshal(r)
	if err := ioutil.WriteFile(path.Join(s.storeDir, "kvass-shard.json"), data, 0755); err != nil {
		return api.InternalErr(err, "save targets to local")
	}

	return api.Data(nil)
}

func (s *Service) doApplyTargets(r *shard.UpdateTargetsRequest) error {
	for _, apply := range s.applyTargets {
		if err := apply(r.Targets); err != nil {
			return err
		}
	}
	s.lg.Infof("targets updated")
	return nil
}

func (s *Service) loadTargets() (*shard.UpdateTargetsRequest, error) {
	_ = os.MkdirAll(s.storeDir, 0755)
	res := &shard.UpdateTargetsRequest{}
	data, err := ioutil.ReadFile(s.storePath())
	if err == nil {
		if err := json.Unmarshal(data, res); err != nil {
			return nil, errors.Wrapf(err, "marshal kvass-shard.json")
		}
	} else {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "load kvass-shard.json failed")
		}
		// compatible old version
		data, err := ioutil.ReadFile(path.Join(s.storeDir, "targets.json"))
		if err != nil {
			if os.IsNotExist(err) {
				return res, nil
			}
			return nil, errors.Wrapf(err, "load targets.json failed")
		}

		if err := json.Unmarshal(data, &res.Targets); err != nil {
			return nil, errors.Wrapf(err, "marshal targets.json")
		}
	}
	return res, nil
}

func (s *Service) storePath() string {
	return path.Join(s.storeDir, "kvass-shard.json")
}
