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
	"github.com/prometheus/prometheus/scrape"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/test"
)

func newTestingGroup(t *testing.T) (*Group, *Replicas, *require.Assertions) {
	lg := logrus.New()
	s := NewGroup("0", lg)
	rep := NewReplicas("r0", "", lg)
	s.AddReplicas(rep)
	return s, rep, require.New(t)
}

func TestGroup_Replicas(t *testing.T) {
	s, rep, r := newTestingGroup(t)
	r.Equal(rep.ID, s.Replicas()[0].ID)
	r.Equal(1, len(s.Replicas()))
}

func TestShard_TargetsScraping(t *testing.T) {
	s, rep, r := newTestingGroup(t)
	rep.APIGet = func(url string, ret interface{}) error {
		return test.CopyJSON(ret, map[uint64]*target.ScrapeStatus{
			1: {
				LastError: "test",
			},
		})
	}

	ret, err := s.TargetsScraping()
	r.NoError(err)
	r.NotNil(ret)
}

func TestGroup_RuntimeInfo(t *testing.T) {
	s, rep, r := newTestingGroup(t)
	rep.APIGet = func(url string, ret interface{}) error {
		return test.CopyJSON(ret, &RuntimeInfo{
			HeadSeries: 10,
		})
	}

	res, err := s.RuntimeInfo()
	r.NoError(err)
	r.Equal(int64(10), res.HeadSeries)
}

func TestGroup_TargetStatus(t *testing.T) {
	s, rep, r := newTestingGroup(t)
	st := &target.ScrapeStatus{
		LastError:          "test",
		LastScrape:         time.Time{},
		LastScrapeDuration: 10,
		Health:             scrape.HealthBad,
		Series:             100,
	}
	rep.APIGet = func(url string, ret interface{}) error {
		return test.CopyJSON(ret, map[uint64]*target.ScrapeStatus{
			1: st,
		})
	}

	ret, err := s.TargetStatus()
	r.NoError(err)
	r.JSONEq(test.MustJSON(st), test.MustJSON(ret[1]))
}

func TestGroup_UpdateTarget(t *testing.T) {
	var cases = []struct {
		name        string
		curScraping map[uint64]bool
		wantTargets *UpdateTargetsRequest
		wantUpdate  bool
	}{
		{
			name:        "need update",
			curScraping: map[uint64]bool{},
			wantTargets: &UpdateTargetsRequest{
				Targets: map[string][]*target.Target{
					"job1": {
						{
							Hash: 1,
						},
					},
				},
			},
			wantUpdate: true,
		},
		{
			name:        "not need update",
			curScraping: map[uint64]bool{1: true},
			wantTargets: &UpdateTargetsRequest{
				Targets: map[string][]*target.Target{
					"job1": {
						{
							Hash: 1,
						},
					},
				},
			},
			wantUpdate: false,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			s, rep, r := newTestingGroup(t)
			rep.scraping = cs.curScraping
			rep.APIPost = func(url string, req interface{}, ret interface{}) (err error) {
				r.True(cs.wantUpdate)
				return nil
			}
			r.NoError(s.UpdateTarget(cs.wantTargets))
		})
	}
}
