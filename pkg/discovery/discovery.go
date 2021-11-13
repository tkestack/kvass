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
	"time"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/target"

	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/scrape"
	"github.com/sirupsen/logrus"
)

// SDTargets represent a target has be processed with job relabel_configs
// every SDTargets contains a target use for shard sidecar to generate prometheus config
// and a prometheus scrape target for api /api/v1/targets
type SDTargets struct {
	// Job if the jobName of this target
	Job string
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

// WaitInit block until all job's sd done
func (m *TargetsDiscovery) WaitInit(ctx context.Context) error {
	t := time.NewTicker(time.Second)
	flag := map[string]bool{}
l1:
	for {
		select {
		case <-t.C:
			m.targetsLock.Lock()
			for job := range m.config {
				if _, exist := m.activeTargets[job]; !exist {
					m.targetsLock.Unlock()
					continue l1
				}
				if !flag[job] {
					m.log.Infof("job %s first service discovery done, active(%d) ,drop(%d)", job, len(m.activeTargets[job]), len(m.dropTargets[job]))
					flag[job] = true
				}
			}
			m.log.Infof("all job first service discovery done")
			m.targetsLock.Unlock()
			return nil
		case <-ctx.Done():
			return nil
		}
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

// ActiveTargetsByHash return a map that with the key of target hash
func (m *TargetsDiscovery) ActiveTargetsByHash() map[uint64]*SDTargets {
	m.targetsLock.Lock()
	defer m.targetsLock.Unlock()
	ret := map[uint64]*SDTargets{}
	for _, ts := range m.activeTargets {
		for _, t := range ts {
			ret[t.ShardTarget.Hash] = t
		}
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
func (m *TargetsDiscovery) ApplyConfig(cfg *prom.ConfigInfo) error {
	m.targetsLock.Lock()
	defer m.targetsLock.Unlock()

	newActiveTargets := map[string][]*SDTargets{}
	newDropTargets := map[string][]*SDTargets{}
	newCfg := map[string]*config.ScrapeConfig{}
	for _, j := range cfg.Config.ScrapeConfigs {
		newCfg[j.JobName] = j
		if _, exist := m.activeTargets[j.JobName]; exist {
			newActiveTargets[j.JobName] = m.activeTargets[j.JobName]
			newDropTargets[j.JobName] = m.dropTargets[j.JobName]
		}
	}
	m.config = newCfg
	m.activeTargets = newActiveTargets
	m.dropTargets = newDropTargets
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
	actives := map[string][]*SDTargets{}
	drops := map[string][]*SDTargets{}
	for job, tsg := range targets {
		allActive := make([]*SDTargets, 0)
		allDrop := make([]*SDTargets, 0)

		cfg := m.config[job]
		if cfg == nil {
			m.log.Warnf("can not found job %m", job)
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
		actives[job] = allActive
		drops[job] = allDrop
	}

	m.targetsLock.Lock()
	defer m.targetsLock.Unlock()

	for job, targets := range actives {
		m.activeTargets[job] = targets
	}

	for job, targets := range drops {
		m.dropTargets[job] = targets
	}

	return actives
}
