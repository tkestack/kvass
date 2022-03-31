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
	"github.com/prometheus/client_golang/prometheus"
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
	configFile       string
	lg               logrus.FieldLogger
	cfgManager       *prom.ConfigManager
	getScrapeStatus  func() map[uint64]*target.ScrapeStatus
	getActiveTargets func() map[string][]*discovery.SDTargets
	getDropTargets   func() map[string][]*discovery.SDTargets
}

// NewService return a new web server
func NewService(
	configFile string,
	cfgManager *prom.ConfigManager,
	getScrapeStatus func() map[uint64]*target.ScrapeStatus,
	getActiveTargets func() map[string][]*discovery.SDTargets,
	getDropTargets func() map[string][]*discovery.SDTargets,
	promRegistry *prometheus.Registry,
	lg logrus.FieldLogger) *Service {

	w := &Service{
		configFile:       configFile,
		Engine:           gin.Default(),
		lg:               lg,
		cfgManager:       cfgManager,
		getScrapeStatus:  getScrapeStatus,
		getActiveTargets: getActiveTargets,
		getDropTargets:   getDropTargets,
	}

	pprof.Register(w.Engine)

	h := api.NewHelper(lg, promRegistry, "kvass_coordinator")
	w.GET("/metrics", h.MetricsHandler)
	w.GET("/api/v1/targets", h.Wrap(w.targets))
	w.GET("/api/v1/shard/runtimeinfo", h.Wrap(w.runtimeInfo))
	w.GET("/api/v1/metricsinfo", h.Wrap(w.metricsInfo))

	w.POST("/-/reload", h.Wrap(func(ctx *gin.Context) *api.Result {
		if err := w.cfgManager.ReloadFromFile(configFile); err != nil {
			return api.BadDataErr(err, "reload failed")
		}
		return api.Data(nil)
	}))

	w.GET("/api/v1/status/config", h.Wrap(func(ctx *gin.Context) *api.Result {
		return api.Data(gin.H{"yaml": string(cfgManager.ConfigInfo().RawContent)})
	}))
	return w
}

func (s *Service) metricsInfo(ctx *gin.Context) *api.Result {
	ret := &MetricsInfo{
		LastSamples: map[string]uint64{},
	}
	for _, ss := range s.getScrapeStatus() {
		for k, v := range ss.LastMetricsSamples {
			ret.LastSamples[k] += v
			ret.SamplesTotal += v
		}
	}
	ret.MetricsTotal = uint64(len(ret.LastSamples))
	return api.Data(ret)
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

// ExtendTarget extend Prometheus v1.Target
type ExtendTarget struct {
	// Target is Prometheus active target from /api/v1/targets
	v1.Target
	// Series is the avg series of last 5 scraping results
	Series int64 `json:"series"`
	// Shards contains ID of shards that is scraping this target
	Shards []string `json:"shards"`
}

// TargetDiscovery has all the active targets.
type TargetDiscovery struct {
	// ActiveTargets contains all targets that should be scraped
	ActiveTargets []*ExtendTarget `json:"activeTargets"`
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
		jobRegexp  []*regexp.Regexp
	)

	if statistics != "" && statistics != "only" && statistics != "with" {
		return api.BadDataErr(fmt.Errorf("wrong param values statistics"), "")
	}

	for _, j := range jobs {
		comp, err := regexp.Compile(j)
		if err != nil {
			return api.BadDataErr(fmt.Errorf("wrong format of job"), "")
		}
		jobRegexp = append(jobRegexp, comp)
	}

	return api.Data(s.getTargets(state, statistics, health, jobRegexp))
}

func (s *Service) getTargets(state string, statistics string, health []string, jobRegexp []*regexp.Regexp) *TargetDiscovery {
	showActive := state == "" || state == "any" || state == "active"
	showDropped := state == "" || state == "any" || state == "dropped"
	res := &TargetDiscovery{}

	if showActive || statistics != "" {
		res.ActiveTargets, res.ActiveStatistics = s.statisticActiveTargets(jobRegexp, health)
		if statistics == "" {
			res.ActiveStatistics = []TargetStatistics{}
		}
	}

	if !showActive || statistics == "only" {
		res.ActiveTargets = []*ExtendTarget{}
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
	return res
}

func (s *Service) statisticActiveTargets(jobRegexp []*regexp.Regexp, health []string) ([]*ExtendTarget, []TargetStatistics) {
	sts := make([]TargetStatistics, 0)
	status := s.getScrapeStatus()
	activeTargets := s.getActiveTargets()
	activeKeys, numTargets := sortKeys(activeTargets)
	targets := make([]*ExtendTarget, 0, numTargets)
	for _, jobName := range activeKeys {
		if filterJobName(jobName, jobRegexp) {
			continue
		}

		jobSts := TargetStatistics{
			JobName: jobName,
			Health:  map[scrape.TargetHealth]uint64{},
		}

		for _, t := range activeTargets[jobName] {
			rt := status[t.ShardTarget.Hash]
			if rt == nil {
				rt = target.NewScrapeStatus(0)
			}
			jobSts.Total++
			jobSts.Health[rt.Health]++

			if len(health) != 0 && !types.FindString(string(rt.Health), health...) {
				continue
			}
			targets = append(targets, makeTarget(jobName, t.PromTarget, rt))
		}

		sts = append(sts, jobSts)
	}
	return targets, sts
}

func sortKeys(targets map[string][]*discovery.SDTargets) ([]string, int) {
	var n int
	keys := make([]string, 0, len(targets))
	for k := range targets {
		keys = append(keys, k)
		n += len(targets[k])
	}
	sort.Strings(keys)
	return keys, n
}

func flatten(targets map[string][]*discovery.SDTargets) []*scrape.Target {
	keys, n := sortKeys(targets)
	res := make([]*scrape.Target, 0, n)
	for _, k := range keys {
		for _, t := range targets[k] {
			res = append(res, t.PromTarget)
		}
	}
	return res
}

func makeTarget(jobName string, target *scrape.Target, rt *target.ScrapeStatus) *ExtendTarget {
	return &ExtendTarget{
		Target: v1.Target{
			DiscoveredLabels:   target.DiscoveredLabels().Map(),
			Labels:             target.Labels().Map(),
			ScrapePool:         jobName,
			ScrapeURL:          target.URL().String(),
			GlobalURL:          target.URL().String(),
			LastError:          rt.LastError,
			LastScrape:         rt.LastScrape,
			LastScrapeDuration: rt.LastScrapeDuration,
			Health:             rt.Health,
		},
		Series: rt.Series,
		Shards: rt.Shards,
	}
}

func filterJobName(jobName string, jobRegexp []*regexp.Regexp) bool {
	ok := false
	for _, job := range jobRegexp {
		if job.MatchString(jobName) {
			ok = true
			break
		}
	}

	return len(jobRegexp) != 0 && !ok
}
