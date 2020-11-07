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

package discovery

import (
	"context"
	"github.com/prometheus/prometheus/config"
	"sync"
	"tkestack.io/kvass/pkg/target"

	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/scrape"
	"github.com/sirupsen/logrus"
)

// SDTargets represent a target has be processed with job relabel_configs
// every SDTargets contains a target use for shard sidecar to generate prometheus config
// and a prometheus scrape target for api /api/v1/targets
type SDTargets struct {
	// ShardTarget is the target for shard sidecar to generate prometheus config
	ShardTarget *target.Target
	// PromTarget is the target of prometheus lib
	PromTarget *scrape.Target
}

// TargetsDiscovery manager the active targets and dropped targets use SD manager from prometheus lib
// it will maintains shard.Target and scrape.Target together
type TargetsDiscovery struct {
	config            map[string]*config.ScrapeConfig
	log               logrus.FieldLogger
	activeTargetsChan chan map[string][]*SDTargets
	activeTargets     map[string][]*SDTargets
	dropTargets       map[string][]*SDTargets
	targetsLock       sync.Mutex
}

// New create a new TargetsDiscovery
func New(log logrus.FieldLogger) *TargetsDiscovery {
	return &TargetsDiscovery{
		log:               log,
		activeTargetsChan: make(chan map[string][]*SDTargets, 1000),
		activeTargets:     map[string][]*SDTargets{},
		dropTargets:       map[string][]*SDTargets{},
	}
}

// ActiveTargetsChan return an channel for notify active SDTargets updated
func (m *TargetsDiscovery) ActiveTargetsChan() <-chan map[string][]*SDTargets {
	return m.activeTargetsChan
}

// ActiveTargets return a copy map of global active targets the
func (m *TargetsDiscovery) ActiveTargets() map[string][]*SDTargets {
	m.targetsLock.Lock()
	defer m.targetsLock.Unlock()

	ret := map[string][]*SDTargets{}
	for k, v := range m.activeTargets {
		ret[k] = v
	}
	return ret
}

// DropTargets return a copy map of global dropped targets the
func (m *TargetsDiscovery) DropTargets() map[string][]*SDTargets {
	m.targetsLock.Lock()
	defer m.targetsLock.Unlock()

	ret := map[string][]*SDTargets{}
	for k, v := range m.dropTargets {
		ret[k] = v
	}
	return ret
}

// ApplyConfig save new scrape config
func (m *TargetsDiscovery) ApplyConfig(cfg *config.Config) error {
	newCfg := map[string]*config.ScrapeConfig{}
	for _, j := range cfg.ScrapeConfigs {
		newCfg[j.JobName] = j
	}
	m.config = newCfg
	return nil
}

// Run receive prometheus service discovery result and update global active SDTargets and dropped SDTargets
// the active SDTargets of one process will be send to activeTargetsChan
func (m *TargetsDiscovery) Run(ctx context.Context, sdChan <-chan map[string][]*targetgroup.Group) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case ts := <-sdChan:
			m.activeTargetsChan <- m.translateTargets(ts)
		}
	}
}

func (m *TargetsDiscovery) translateTargets(targets map[string][]*targetgroup.Group) map[string][]*SDTargets {
	res := map[string][]*SDTargets{}
	for job, tsg := range targets {
		allActive := make([]*SDTargets, 0)
		allDrop := make([]*SDTargets, 0)

		cfg := m.config[job]
		if cfg == nil {
			m.log.Errorf("can not found job %m", job)
			continue
		}

		for _, tr := range tsg {
			ts, err := targetsFromGroup(tr, cfg)
			if err != nil {
				m.log.Error("create target for job", cfg.JobName, err.Error())
				continue
			}

			for _, tar := range ts {
				if tar.PromTarget.Labels().Len() > 0 {
					allActive = append(allActive, tar)
				} else if tar.PromTarget.DiscoveredLabels().Len() > 0 {
					allDrop = append(allDrop, tar)
				}
			}
		}
		res[job] = allActive
		m.activeTargets[job] = allActive
		m.dropTargets[job] = allDrop
	}

	return res
}
