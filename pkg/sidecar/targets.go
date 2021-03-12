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
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/types"
)

var (
	storeFileName           = "kvass-shard.json"
	oldVersionStoreFileName = "targets.json"
)

// TargetsInfo contains all current targets
type TargetsInfo struct {
	// Targets is all targets this shard scraping
	Targets map[string][]*target.Target
	// IdleAt is the time this shard has no scraping targets
	// IdleAt is nil if at lease one target is scraping
	IdleAt *time.Time
	// Status is the runtime status of all targets
	Status map[uint64]*target.ScrapeStatus `json:"-"`
	globalTargetMap map[uint64]bool
}

func newTargetsInfo() TargetsInfo {
	return TargetsInfo{
		Targets: map[string][]*target.Target{},
		Status:  map[uint64]*target.ScrapeStatus{},
	}
}

// TargetsManager manager local targets of this shard
type TargetsManager struct {
	targets         TargetsInfo
	updateCallbacks []func(targets map[string][]*target.Target) error
	storeDir        string
	log             logrus.FieldLogger
}

// NewTargetsManager return a new target manager
func NewTargetsManager(storeDir string, log logrus.FieldLogger) *TargetsManager {
	return &TargetsManager{
		storeDir: storeDir,
		log:      log,
		targets:  newTargetsInfo(),
	}
}

// Load load local targets information from storeDir
func (t *TargetsManager) Load() error {
	_ = os.MkdirAll(t.storeDir, 0755)
	data, err := ioutil.ReadFile(t.storePath())
	if err == nil {
		if err := json.Unmarshal(data, &t.targets); err != nil {
			return errors.Wrapf(err, "marshal %s", storeFileName)
		}
	} else {
		if !os.IsNotExist(err) {
			return errors.Wrapf(err, "load %s failed", storeFileName)
		}
		// compatible old version
		data, err := ioutil.ReadFile(path.Join(t.storeDir, oldVersionStoreFileName))
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return errors.Wrapf(err, "load %s failed", oldVersionStoreFileName)
		}

		if err := json.Unmarshal(data, &t.targets.Targets); err != nil {
			return errors.Wrapf(err, "marshal targets.json")
		}
	}

	return t.UpdateTargets(&shard.UpdateTargetsRequest{Targets: t.targets.Targets})
}

// AddUpdateCallbacks add a call back for targets updating event
func (t *TargetsManager) AddUpdateCallbacks(f ...func(targets map[string][]*target.Target) error) {
	t.updateCallbacks = append(t.updateCallbacks, f...)
}

// UpdateTargets update local targets
func (t *TargetsManager) UpdateTargets(req *shard.UpdateTargetsRequest) error {
	t.targets.Targets = req.Targets
	t.updateStatus()
	t.updateIdleState()

	if err := t.doCallbacks(); err != nil {
		return errors.Wrapf(err, "do callbacks")
	}

	return errors.Wrapf(t.saveTargets(), "save targets to file")
}

func (t *TargetsManager) updateIdleState() {
	t.log.Info(fmt.Sprintf("Status len: %d", len(t.targets.Status)))
	t.log.Info(fmt.Sprintf("Global target len: %d", len(t.targets.globalTargetMap)))
	normalTarNum := len(t.targets.Status) - len(t.targets.globalTargetMap)
	if normalTarNum == 0 && t.targets.IdleAt == nil {
		t.targets.IdleAt = types.TimePtr(time.Now())
		t.log.Info(fmt.Sprintf("Shard is idle. Time: %s", t.targets.IdleAt.String()))
	}

	if normalTarNum != 0 {
		t.targets.IdleAt = nil
	}
}

func (t *TargetsManager) updateStatus() {
	status := map[uint64]*target.ScrapeStatus{}
	globalTargetMap := map[uint64]bool{}
	for job, ts := range t.targets.Targets {
		for _, tar := range ts {
			if strings.HasPrefix(job, "kvass_global_") {
				globalTargetMap[tar.Hash] = true
			}
			if t.targets.Status[tar.Hash] == nil {
				status[tar.Hash] = target.NewScrapeStatus(tar.Series)
			} else {
				status[tar.Hash] = t.targets.Status[tar.Hash]
			}
			if status[tar.Hash].TargetState == target.StateNormal && tar.TargetState == target.StateInTransfer {
				t.log.Infof("%s/%s begin transfer", job, tar.NoParamURL())
				status[tar.Hash].ScrapeTimes = 0
			}

			status[tar.Hash].TargetState = tar.TargetState
		}
	}
	t.targets.Status = status
	t.targets.globalTargetMap = globalTargetMap
}

func (t *TargetsManager) doCallbacks() error {
	for _, call := range t.updateCallbacks {
		if err := call(t.targets.Targets); err != nil {
			return err
		}
	}
	return nil
}

func (t *TargetsManager) saveTargets() error {
	data, _ := json.Marshal(&t.targets)
	if err := ioutil.WriteFile(t.storePath(), data, 0755); err != nil {
		return err
	}
	return nil
}

func (t *TargetsManager) storePath() string {
	return path.Join(t.storeDir, storeFileName)
}

// TargetsInfo return current targets of this shard
func (t *TargetsManager) TargetsInfo() TargetsInfo {
	return t.targets
}
