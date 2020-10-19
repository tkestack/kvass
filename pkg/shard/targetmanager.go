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
	"sync"

	log "github.com/sirupsen/logrus"
)

// TargetManager manage all targets information
type TargetManager struct {
	targets map[string]map[string]*Target
	lastErr sync.Map
	log     log.FieldLogger
}

func NewTargetManager(log log.FieldLogger) *TargetManager {
	return &TargetManager{
		log:     log,
		targets: map[string]map[string]*Target{},
	}
}

// GetLastErr return the lastErr saved
func (m *TargetManager) GetLastErr(hash string) string {
	err, exist := m.lastErr.Load(hash)
	if !exist {
		return ""
	}
	return err.(string)
}

// Get try found target with specific job and url
// nil will be return if no target found
func (m *TargetManager) Get(job string, url string) *Target {
	if m.targets[job] != nil && m.targets[job][url] != nil {
		return m.targets[job][url]
	}
	return nil
}

// Update update all TargetManager targets
func (m *TargetManager) Update(targets map[string][]*Target, groupID string) {
	mp := make(map[string]map[string]*Target)
	total := 0
	scraping := 0
	for id, ts := range targets {
		for _, t := range ts {
			total++
			t.shardId = id
			if id == groupID {
				t.scraping = true
				scraping++
				old := m.Get(t.JobName, t.URL)
				if old != nil {
					t.lastSamples = old.lastSamples
				}
			}

			if mp[t.JobName] == nil {
				mp[t.JobName] = map[string]*Target{}
			}
			mp[t.JobName][t.URL] = t
		}
	}

	m.targets = mp
}
