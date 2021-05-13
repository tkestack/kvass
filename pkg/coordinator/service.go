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
	"fmt"
	"regexp"
	"sort"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/utils/types"

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
	w.GET("/api/v1/shard/runtimeinfo", api.Wrap(lg, w.runtimeInfo))

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

// runtimeInfo return statistics runtimeInfo of all shards
func (s *Service) runtimeInfo(ctx *gin.Context) *api.Result {
	series := int64(0)
	for _, st := range s.getScrapeStatus() {
		series += st.Series
	}

	return api.Data(&shard.RuntimeInfo{
		ConfigHash: s.cfgManager.ConfigInfo().ConfigHash,
		HeadSeries: series,
	})
}

// TargetDiscovery has all the active targets.
type TargetDiscovery struct {
	// ActiveTargets contains all targets that should be scraped
	ActiveTargets []*v1.Target `json:"activeTargets"`
	// ActiveStatistics contains job's statistics number according to target health
	ActiveStatistics []TargetStatistics `json:"activeStatistics,omitempty"`
	// DroppedTargets contains all targets that been dropped from relabel
	DroppedTargets []*v1.DroppedTarget `json:"droppedTargets"`
}

// TargetStatistics contains statistics number according to target health
type TargetStatistics struct {
	// JobName is the job name of this statistics
	JobName string
	// Total is all active targets number
	Total uint64
	// Health contains the number of every health status
	Health map[scrape.TargetHealth]uint64
}

// targets compatible of prometheus Service /api/v1/targets
// targets combines targets information from service discovery, sidecar and exploring
// we support some extend query param:
// - state: (compatible)
// - statistics: "only" (return statistics only), "with" (return statistics and targets list)
// - job: "job_name" (return active targets with specific job_name, regexp is supported)
// - health: "up", "down", "unknown" (return active targets with specific health status)
func (s *Service) targets(ctx *gin.Context) *api.Result {
	var (
		state      = ctx.Query("state")
		statistics = ctx.Query("statistics")
		jobs       = ctx.QueryArray("job")
		health     = ctx.QueryArray("health")
		jobRexg    []*regexp.Regexp
	)

	if statistics != "" && statistics != "only" && statistics != "with" {
		return api.BadDataErr(fmt.Errorf("wrong param values statistics"), "")
	}

	for _, j := range jobs {
		comp, err := regexp.Compile(j)
		if err != nil {
			return api.BadDataErr(fmt.Errorf("wrong format of job"), "")
		}
		jobRexg = append(jobRexg, comp)
	}

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
	res := &TargetDiscovery{}

	if showActive || statistics != "" {
		activeTargets := s.getActiveTargets()
		activeKeys, numTargets := sortKeys(activeTargets)
		res.ActiveTargets = make([]*v1.Target, 0, numTargets)
		status := s.getScrapeStatus()

		sts := make([]TargetStatistics, 0)
		for _, key := range activeKeys {
			ok := false
			for _, job := range jobRexg {
				if job.MatchString(key) {
					ok = true
					break
				}
			}

			if len(jobRexg) != 0 && !ok {
				continue
			}

			jobSts := TargetStatistics{
				JobName: key,
				Health:  map[scrape.TargetHealth]uint64{},
			}

			for _, t := range activeTargets[key] {
				tar := t.PromTarget
				hash := t.ShardTarget.Hash
				rt := status[hash]
				if rt == nil {
					rt = target.NewScrapeStatus(0)
				}
				jobSts.Total++
				jobSts.Health[rt.Health]++

				if len(health) != 0 && !types.FindString(string(rt.Health), health...) {
					continue
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

			sts = append(sts, jobSts)
		}

		if statistics != "" {
			res.ActiveStatistics = sts
		}
	}

	if !showActive || statistics == "only" {
		res.ActiveTargets = []*v1.Target{}
	}

	if showDropped && statistics != "only" {
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
