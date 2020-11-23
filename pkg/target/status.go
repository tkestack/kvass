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

package target

import (
	"github.com/prometheus/prometheus/scrape"
	"time"
)

// ScrapeStatus contains last scraping status of the target
type ScrapeStatus struct {
	// LastError save the error string if last scraping is error
	LastError string `json:"lastError"`
	// LastScrape save the time of last scraping
	LastScrape time.Time `json:"lastScrape"`
	// LastScrapeDuration save the seconds duration last scraping spend
	LastScrapeDuration float64 `json:"lastScrapeDuration"`
	// Health it the status of last scraping
	Health scrape.TargetHealth `json:"health"`
	// Series is the avg sample of last 3 times scraping, metrics_relabel_configs will be process
	Series     int64 `json:"samples"`
	lastSeries []int64
}

// SetScrapeErr mark the result of this scraping
// health will be down if err is not nil
// health will be up if err is nil
func (t *ScrapeStatus) SetScrapeErr(start time.Time, err error) {
	t.LastScrape = start
	t.LastScrapeDuration = time.Now().Sub(start).Seconds()
	if err == nil {
		t.LastError = ""
		t.Health = scrape.HealthGood
	} else {
		t.LastError = err.Error()
		t.Health = scrape.HealthBad
	}
}

// NewScrapeStatus create a new ScrapeStatus with referential Series
func NewScrapeStatus(series int64) *ScrapeStatus {
	return &ScrapeStatus{
		Series: series,
		Health: scrape.HealthUnknown,
	}
}

// UpdateSamples statistic target samples info
func (t *ScrapeStatus) UpdateSamples(s int64) {
	if len(t.lastSeries) < 3 {
		t.lastSeries = append(t.lastSeries, s)
	} else {
		newSeries := make([]int64, 0)
		newSeries = append(newSeries, t.lastSeries[1:]...)
		newSeries = append(newSeries, s)
		t.lastSeries = newSeries
	}

	total := int64(0)
	for _, i := range t.lastSeries {
		total += i
	}

	t.Series = int64(float64(total) / float64(len(t.lastSeries)))
}
