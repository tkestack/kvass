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
	"net/url"
	"strings"
	"testing"
	"time"

	kscrape "tkestack.io/kvass/pkg/scrape"

	"github.com/prometheus/prometheus/scrape"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"tkestack.io/kvass/pkg/target"
	"tkestack.io/kvass/pkg/utils/test"
)

func newTestingShard(t *testing.T) (*Shard, *require.Assertions) {
	lg := logrus.New()
	s := NewShard("0", "", true, lg)
	return s, require.New(t)
}

func TestShard_RuntimeInfo(t *testing.T) {
	s, r := newTestingShard(t)
	s.APIGet = func(url string, ret interface{}) error {
		return test.CopyJSON(ret, &RuntimeInfo{
			HeadSeries: 10,
		})
	}

	res, err := s.RuntimeInfo()
	r.NoError(err)
	r.Equal(int64(10), res.HeadSeries)
}

func TestShard_TargetStatus(t *testing.T) {
	s, r := newTestingShard(t)
	st := &target.ScrapeStatus{
		LastError:          "test",
		LastScrape:         time.Time{},
		LastScrapeDuration: 10,
		Health:             scrape.HealthBad,
		Series:             100,
	}
	s.APIGet = func(url string, ret interface{}) error {
		return test.CopyJSON(ret, map[uint64]*target.ScrapeStatus{
			1: st,
		})
	}

	ret, err := s.TargetStatus()
	r.NoError(err)
	r.JSONEq(test.MustJSON(st), test.MustJSON(ret[1]))
}

func TestShard_UpdateTarget(t *testing.T) {
	var cases = []struct {
		name        string
		curScraping map[uint64]*target.ScrapeStatus
		wantTargets *UpdateTargetsRequest
		wantUpdate  bool
	}{
		{
			name:        "need update, targets not exist",
			curScraping: map[uint64]*target.ScrapeStatus{},
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
			name: "need update, target state change",
			curScraping: map[uint64]*target.ScrapeStatus{
				1: {},
			},
			wantTargets: &UpdateTargetsRequest{
				Targets: map[string][]*target.Target{
					"job1": {
						{
							Hash:        1,
							TargetState: target.StateInTransfer,
						},
					},
				},
			},
			wantUpdate: true,
		},
		{
			name: "not need update",
			curScraping: map[uint64]*target.ScrapeStatus{
				1: {},
			},
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
			s, r := newTestingShard(t)
			s.scraping = cs.curScraping
			s.APIPost = func(url string, req interface{}, ret interface{}) (err error) {
				r.True(cs.wantUpdate)
				return nil
			}
			r.NoError(s.UpdateTarget(cs.wantTargets))
		})
	}
}

func TestShard_Samples(t *testing.T) {
	fakeGet := func(u string, ret interface{}) error {
		ul, err := url.Parse(u)
		if err != nil {
			return err
		}

		job := ul.Query().Get("job")
		detail := ul.Query().Get("with_metrics_detail")
		data := map[string]*kscrape.StatisticsSeriesResult{}
		res := kscrape.NewStatisticsSeriesResult()
		res.ScrappedTotal = 1
		if detail == "true" {
			res.MetricsTotal = map[string]*kscrape.MetricSamplesInfo{
				"test": {
					Total:    2,
					Scrapped: 1,
				},
			}
		}
		if job == "" || strings.Contains(job, "job1") {
			data["job1"] = res
		}

		test.CopyJSON(ret, data)
		return nil
	}

	var cases = []struct {
		desc       string
		jobName    string
		withDetail bool
		wantResult map[string]*kscrape.StatisticsSeriesResult
	}{
		{
			desc:       "without job name, without metrics detail",
			jobName:    "",
			withDetail: false,
			wantResult: map[string]*kscrape.StatisticsSeriesResult{
				"job1": {
					ScrappedTotal: 1,
					MetricsTotal:  map[string]*kscrape.MetricSamplesInfo{},
				},
			},
		},
		{
			desc:       "with wrong job name filter",
			jobName:    "xx",
			withDetail: false,
			wantResult: map[string]*kscrape.StatisticsSeriesResult{},
		}, {
			desc:       "with right job name filter",
			jobName:    "job1",
			withDetail: false,
			wantResult: map[string]*kscrape.StatisticsSeriesResult{
				"job1": {
					ScrappedTotal: 1,
					MetricsTotal:  map[string]*kscrape.MetricSamplesInfo{},
				},
			},
		},
		{
			desc:       "without job name,  with metrics detail",
			withDetail: true,
			wantResult: map[string]*kscrape.StatisticsSeriesResult{
				"job1": {
					ScrappedTotal: 1,
					MetricsTotal: map[string]*kscrape.MetricSamplesInfo{
						"test": {
							Total:    2,
							Scrapped: 1,
						},
					},
				},
			},
		},
	}

	for _, cs := range cases {
		t.Run(cs.desc, func(t *testing.T) {
			s, r := newTestingShard(t)
			s.APIGet = fakeGet
			res, err := s.Samples(cs.jobName, cs.withDetail)
			r.NoError(err)
			r.JSONEq(test.MustJSON(cs.wantResult), test.MustJSON(res))
		})
	}
}
