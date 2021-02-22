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
	"sort"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/prometheus/scrape"
	v1 "github.com/prometheus/prometheus/web/api/v1"
	"github.com/sirupsen/logrus"

	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/target"
)

// Service is the api server of coordinator
type Service struct {
	// gin.Engine is the gin engine for handle http request
	*gin.Engine
	lg               logrus.FieldLogger
	cfgManager       *prom.ConfigManager
	getScrapeStatus  func() map[uint64]*target.ScrapeStatus
	getActiveTargets func() map[string][]*discovery.SDTargets
	getDropTargets   func() map[string][]*discovery.SDTargets
}

// NewService return a new web server
func NewService(
	cfgManager *prom.ConfigManager,
	getScrapeStatus func() map[uint64]*target.ScrapeStatus,
	getActiveTargets func() map[string][]*discovery.SDTargets,
	getDropTargets func() map[string][]*discovery.SDTargets,
	lg logrus.FieldLogger) *Service {
	w := &Service{
		Engine:           gin.Default(),
		lg:               lg,
		cfgManager:       cfgManager,
		getScrapeStatus:  getScrapeStatus,
		getActiveTargets: getActiveTargets,
		getDropTargets:   getDropTargets,
	}
	pprof.Register(w.Engine)

	w.GET("/api/v1/targets", api.Wrap(lg, w.targets))
	w.POST("/-/reload", api.Wrap(lg, func(ctx *gin.Context) *api.Result {
		if err := w.cfgManager.Reload(); err != nil {
			return api.BadDataErr(err, "reload failed")
		}
		return api.Data(nil)
	}))
	w.GET("/api/v1/status/config", api.Wrap(lg, func(ctx *gin.Context) *api.Result {
		return api.Data(gin.H{"yaml": string(cfgManager.ConfigInfo().RawContent)})
	}))
	return w
}

// targets compatible of prometheus Service /api/v1/targets
// targets combines targets information from service discovery, sidecar and exploring
func (s *Service) targets(ctx *gin.Context) *api.Result {
	state := ctx.Query("state")
	sortKeys := func(targets map[string][]*discovery.SDTargets) ([]string, int) {
		var n int
		keys := make([]string, 0, len(targets))
		for k := range targets {
			keys = append(keys, k)
			n += len(targets[k])
		}
		sort.Strings(keys)
		return keys, n
	}

	flatten := func(targets map[string][]*discovery.SDTargets) []*scrape.Target {
		keys, n := sortKeys(targets)
		res := make([]*scrape.Target, 0, n)
		for _, k := range keys {
			for _, t := range targets[k] {
				res = append(res, t.PromTarget)
			}
		}
		return res
	}

	showActive := state == "" || state == "any" || state == "active"
	showDropped := state == "" || state == "any" || state == "dropped"
	res := &v1.TargetDiscovery{}

	if showActive {
		activeTargets := s.getActiveTargets()
		activeKeys, numTargets := sortKeys(activeTargets)
		res.ActiveTargets = make([]*v1.Target, 0, numTargets)
		status := s.getScrapeStatus()

		for _, key := range activeKeys {
			for _, t := range activeTargets[key] {
				tar := t.PromTarget
				hash := t.ShardTarget.Hash
				rt := status[hash]
				if rt == nil {
					rt = target.NewScrapeStatus(0)
				}

				res.ActiveTargets = append(res.ActiveTargets, &v1.Target{
					DiscoveredLabels:   tar.DiscoveredLabels().Map(),
					Labels:             tar.Labels().Map(),
					ScrapePool:         key,
					ScrapeURL:          tar.URL().String(),
					GlobalURL:          tar.URL().String(),
					LastError:          rt.LastError,
					LastScrape:         rt.LastScrape,
					LastScrapeDuration: rt.LastScrapeDuration,
					Health:             rt.Health,
				})
			}
		}
	} else {
		res.ActiveTargets = []*v1.Target{}
	}
	if showDropped {
		tDropped := flatten(s.getDropTargets())
		res.DroppedTargets = make([]*v1.DroppedTarget, 0, len(tDropped))
		for _, t := range tDropped {
			res.DroppedTargets = append(res.DroppedTargets, &v1.DroppedTarget{
				DiscoveredLabels: t.DiscoveredLabels().Map(),
			})
		}
	} else {
		res.DroppedTargets = []*v1.DroppedTarget{}
	}

	return api.Data(res)
}
