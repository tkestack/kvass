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

package shard

import (
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/scrape"
	v1 "github.com/prometheus/prometheus/web/api/v1"
	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/prom"
)

// RuntimeManager manager shard runtime status such as HeadSeries, shardID
type RuntimeManager struct {
	*prom.ScrapeInfos
	cur        *RuntimeInfo
	tarManager *TargetManager
	promCli    prom.Client
	log        logrus.FieldLogger
}

// NewRuntimeManager create a new RuntimeManager
func NewRuntimeManager(
	tarManager *TargetManager,
	promCli prom.Client,
	log logrus.FieldLogger,
) *RuntimeManager {
	return &RuntimeManager{
		ScrapeInfos: prom.NewScrapInfos(nil),
		cur:         &RuntimeInfo{},
		tarManager:  tarManager,
		promCli:     promCli,
		log:         log,
	}
}

// RuntimeInfo return the running information about this shard
// HeadSeries is fetched from prometheus, and targets are fetched from prometheus and local target manager
func (r *RuntimeManager) RuntimeInfo() (*RuntimeInfo, error) {
	prt, err := r.promCli.RuntimeInfo()
	if err != nil {
		return nil, errors.Wrapf(err, "get prometheus Runtime info failed")
	}

	targets, err := r.promCli.Targets("active")
	if err != nil {
		return nil, errors.Wrapf(err, "get targets from prometheus failed")
	}

	ret := &RuntimeInfo{
		ID:         r.cur.ID,
		HeadSeries: prt.TimeSeriesCount,
		Targets:    map[string][]*Target{},
	}

	for _, target := range targets.ActiveTargets {
		job := target.DiscoveredLabels["job"]
		jobInfo := r.ScrapeInfos.Get(job)
		if jobInfo == nil {
			r.log.Errorf("can not found job info of %s", job)
			continue
		}

		tt := TargetFromProm(jobInfo.Config, target)
		localTarget := r.tarManager.Get(job, tt.URL)
		id := IDUnknown
		if localTarget != nil {
			id = localTarget.shardID
			tt.Samples = localTarget.Samples
			// if target belongs to this shard but never scraped
			// we should add the samples to HeadSeries to predict the series increment
			// and we should set Healthy to HealthUnknown
			if localTarget.scraping && len(localTarget.lastSamples) == 0 {
				ret.HeadSeries += localTarget.Samples
			}

			// if target is not belongs to this shard, we should get Healthy from localTarget target, which get from coordinator
			if !localTarget.scraping {
				tt.Healthy = localTarget.Healthy
			}
		} else {
			tt.Healthy = string(scrape.HealthUnknown)
		}

		ret.Targets[id] = append(ret.Targets[id], tt)
	}

	return ret, nil
}

// Update update current runtimeInfo
func (r *RuntimeManager) Update(info *RuntimeInfo) error {
	r.tarManager.Update(info.Targets, info.ID)
	r.cur = info
	return nil
}

// Targets return recovered prometheus discovery targets
// origin targets fetched from prometheus is not correct since the config prometheus used is injected
// we should recover targets information to get right target with no injected config
func (r *RuntimeManager) Targets(state string) (*v1.TargetDiscovery, error) {
	targets, err := r.promCli.Targets(state)
	if err != nil {
		return nil, err
	}

	result := &v1.TargetDiscovery{
		DroppedTargets: []*v1.DroppedTarget{},
		ActiveTargets:  []*v1.Target{},
	}

	for _, t := range targets.ActiveTargets {
		job := r.ScrapeInfos.Get(t.ScrapePool)
		if job == nil {
			continue
		}

		// recover url
		u := OriginURL(job.Config, t.ScrapeURL)
		t.GlobalURL = OriginURL(job.Config, t.GlobalURL)
		t.ScrapeURL = u
		tt := r.tarManager.Get(t.ScrapePool, u)
		if tt == nil || !tt.scraping {
			continue
		}

		// recover labels
		t.DiscoveredLabels["__scheme__"] = job.Config.Scheme
		delete(t.DiscoveredLabels, "__param__jobName")

		// recover Health
		if t.Health != "up" {
			t.LastError = tt.lastErr
		}
		result.ActiveTargets = append(result.ActiveTargets, t)
	}

	for _, t := range targets.DroppedTargets {
		jobName := t.DiscoveredLabels["__param__jobName"]
		job := r.ScrapeInfos.Get(jobName)
		if job == nil {
			continue
		}

		t.DiscoveredLabels["__scheme__"] = job.Config.Scheme
		delete(t.DiscoveredLabels, "__param__jobName")
		result.DroppedTargets = append(result.DroppedTargets, t)
	}

	return result, nil
}
