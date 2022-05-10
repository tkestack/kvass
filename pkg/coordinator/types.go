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

// SamplesInfo contains statistic of sample scraped rate
type SamplesInfo struct {
	// SamplesRate is total sample rate in last scrape
	SamplesRate uint64 `json:"samplesRate"`
	// JobsSamplesRate show total sample rate in last scrape about a job
	JobsSamplesRate []*JobSamplesInfo `json:"jobsSamplesRate"`
}

// JobSamplesInfo show total sample rate in last scrape
type JobSamplesInfo struct {
	// JobName is the name of this job
	JobName string `json:"jobName"`
	// SamplesRateTotal is the total samples rate of this job' targets
	SamplesRate uint64 `json:"samplesRateTotal"`
	// MetricsSamplesRate indicate the metrics samples rate
	MetricsSamplesRate map[string]uint64 `json:"metricsSamplesRate"`
}

type space struct {
	headSpace    int64
	processSpace int64
}

func (s *space) add(src space) {
	s.headSpace += src.headSpace
	s.processSpace += src.processSpace
}

func (s *space) isZero() bool {
	return s.headSpace == 0 && s.processSpace == 0
}
