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
	"io/ioutil"
	"path"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/test"
	"tkestack.io/kvass/pkg/utils/types"
)

func TestTargetsManager_Load(t *testing.T) {
	tn := time.Now()
	timeNow = func() time.Time {
		return tn
	}

	cases := []struct {
		name            string
		fileName        string
		storeContent    string
		wantTargetsInfo TargetsInfo
		wantErr         bool
	}{
		{
			name:     "all file not exist, don't return err",
			fileName: "", // empty fileName means file not exist
			wantTargetsInfo: TargetsInfo{
				Targets: map[string][]*target.Target{},
				IdleAt:  types.TimePtr(tn),
				Status:  map[uint64]*target.ScrapeStatus{},
			},
			wantErr: false,
		},
		{
			name:         "old version file exist, not format wrong, must return err ",
			fileName:     oldVersionStoreFileName,
			storeContent: "xxx",
			wantErr:      true,
		},
		{
			name:         "old version file exist, valid content",
			fileName:     oldVersionStoreFileName,
			storeContent: `{"test":[{"Hash":1,"TargetState":"in_transfer","Series":1}]}`,
			wantTargetsInfo: TargetsInfo{
				Targets: map[string][]*target.Target{
					"test": {
						{
							Hash:        1,
							Series:      1,
							TargetState: target.StateInTransfer,
						},
					},
				},
				Status: map[uint64]*target.ScrapeStatus{
					1: target.NewScrapeStatus(1, 1),
				},
			},
		},
		{
			name:         "new version file exist, valid content",
			fileName:     storeFileName,
			storeContent: `{"Targets":{"test":[{"Hash":1,"TargetState":"in_transfer","Series":1}]}}`,
			wantTargetsInfo: TargetsInfo{
				Targets: map[string][]*target.Target{
					"test": {
						{
							Hash:        1,
							Series:      1,
							TargetState: target.StateInTransfer,
						},
					},
				},
				Status: map[uint64]*target.ScrapeStatus{
					1: target.NewScrapeStatus(1, 1),
				},
			},
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			tm := NewTargetsManager(dir, prometheus.NewRegistry(), logrus.New())
			if cs.fileName != "" {
				r.NoError(ioutil.WriteFile(path.Join(dir, cs.fileName), []byte(cs.storeContent), 0755))
			}

			if cs.wantErr {
				r.Error(tm.Load())
				return
			}

			r.NoError(tm.Load())
			ts := tm.TargetsInfo()
			r.JSONEq(test.MustJSON(&cs.wantTargetsInfo), test.MustJSON(&ts))
		})
	}
}

func TestTargetsManager_UpdateTargets(t *testing.T) {
	cases := []struct {
		name            string
		oldTargetsInfo  TargetsInfo
		req             *shard.UpdateTargetsRequest
		wantTargetsInfo TargetsInfo
	}{
		{
			name: "create new target status, become not idle",
			oldTargetsInfo: TargetsInfo{
				IdleAt: types.TimePtr(time.Now()),
			},
			req: &shard.UpdateTargetsRequest{
				Targets: map[string][]*target.Target{
					"test": {
						{
							Hash:   1,
							Series: 1,
						},
					},
				},
			},
			wantTargetsInfo: TargetsInfo{
				Targets: map[string][]*target.Target{
					"test": {
						{
							Hash:   1,
							Series: 1,
						},
					},
				},
				Status: map[uint64]*target.ScrapeStatus{
					1: target.NewScrapeStatus(1, 1),
				},
			},
		},
		{
			name: "delete invalid target status, become idle",
			oldTargetsInfo: TargetsInfo{
				Targets: map[string][]*target.Target{
					"test": {
						{
							Hash:   1,
							Series: 1,
						},
					},
				},
				Status: map[uint64]*target.ScrapeStatus{
					1: target.NewScrapeStatus(1, 1),
				},
			},
			req: &shard.UpdateTargetsRequest{
				Targets: map[string][]*target.Target{},
			},
			wantTargetsInfo: TargetsInfo{
				Targets: map[string][]*target.Target{},
				IdleAt:  types.TimePtr(time.Now()),
				Status:  map[uint64]*target.ScrapeStatus{},
			},
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()

			tm := NewTargetsManager(dir, prometheus.NewRegistry(), logrus.New())
			tm.targets = cs.oldTargetsInfo
			r.NoError(tm.UpdateTargets(cs.req))
			r.JSONEq(test.MustJSON(cs.wantTargetsInfo.Targets), test.MustJSON(tm.targets.Targets))
			r.JSONEq(test.MustJSON(cs.wantTargetsInfo.Status), test.MustJSON(tm.targets.Status))
			r.Equal(cs.wantTargetsInfo.IdleAt == nil, tm.targets.IdleAt == nil)
		})
	}
}

func TestTargetsManager_AddUpdateCallbacks(t *testing.T) {
	cases := []struct {
		name     string
		callBack func(targets map[string][]*target.Target) error
		wantErr  bool
	}{
		{
			name: "no err",
			callBack: func(targets map[string][]*target.Target) error {
				return nil
			},
			wantErr: false,
		},
		{
			name: "want err",
			callBack: func(targets map[string][]*target.Target) error {
				return fmt.Errorf("test")
			},
			wantErr: true,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			tm := NewTargetsManager(t.TempDir(), prometheus.NewRegistry(), logrus.New())

			req := &shard.UpdateTargetsRequest{
				Targets: map[string][]*target.Target{
					"test": {
						{
							Hash:   1,
							Series: 1,
						},
					},
				},
			}

			called := false
			tm.AddUpdateCallbacks(func(targets map[string][]*target.Target) error {
				called = true
				r.JSONEq(test.MustJSON(req.Targets), test.MustJSON(targets))
				return nil
			})
			tm.AddUpdateCallbacks(cs.callBack)

			err := tm.UpdateTargets(req)
			r.True(called)
			r.Equal(cs.wantErr, err != nil)
		})
	}
}
