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
	"fmt"
	"net/url"
	url2 "net/url"

	v1 "github.com/prometheus/prometheus/web/api/v1"

	"github.com/prometheus/prometheus/config"
)

const (
	// TargetMaxRecordSamples indicate how many samples info should be record
	TargetMaxRecordSamples = 3
	IDUnknown              = "__unknown__"
)

// runtimeInfo contains all running status of this shard
type RuntimeInfo struct {
	// ID is unique id of shard
	ID string `json:"shardId,omitempty"`
	// HeadSeries return current head_series of prometheus
	// shard must set this field
	HeadSeries int64 `json:"headSeries,omitempty"`
	// targets include all targets this shard known
	// targets info may get from coordinator or local prometheus
	// map key is ShardsGroup ID or __unknown__ if target is new
	Targets map[string][]*Target `json:"targets"`
}

// Target is the target basic info
type Target struct {
	// JobName is the origin job of target
	JobName string `json:"jobName"`
	// URL is full url of the target
	URL string `json:"url"`
	// Healthy indicates target last tManager healthy
	Healthy string `json:"healthy,omitempty"`
	// Samples is the avg samples of nearly TargetMaxRecordSamples scrapes
	Samples int64 `json:"samples"`

	// lastSamples record recent TargetMaxRecordSamples times samples
	lastSamples []int64
	shardId     string
	scraping    bool
	lastErr     string
}

func (t *Target) setLastErr(err string) {
	t.lastErr = err
}

// Hash return the unique hash of target
func (t *Target) Hash() string {
	return fmt.Sprintf("%s/%s", t.JobName, t.URL)
}

// UpdateSamples statistic target samples info
func (t *Target) UpdateSamples(s int64) {
	if len(t.lastSamples) < TargetMaxRecordSamples {
		t.lastSamples = append(t.lastSamples, s)
	} else {
		newSamples := make([]int64, 0)
		newSamples = append(newSamples, t.lastSamples[1:]...)
		newSamples = append(newSamples, s)
		t.lastSamples = newSamples
	}

	total := int64(0)
	for _, i := range t.lastSamples {
		total += i
	}

	t.Samples = int64(float64(total) / float64(len(t.lastSamples)))
}

// TargetFromProm create new shard Target from prometheus ActiveTarget
func TargetFromProm(jobInfo *config.ScrapeConfig, t *v1.Target) *Target {
	return &Target{
		JobName: jobInfo.JobName,
		URL:     OriginURL(jobInfo, t.ScrapeURL),
		Samples: -1,
		Healthy: string(t.Health),
	}
}

func OriginURL(jobInfo *config.ScrapeConfig, oldURL string) string {
	u, _ := url.Parse(oldURL)
	m, _ := url2.ParseQuery(u.RawQuery)
	m.Del(jobNameFormName)
	u.RawQuery = m.Encode()
	u.Scheme = jobInfo.Scheme
	return u.String()
}
