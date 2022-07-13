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
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/scrape"
	"github.com/stretchr/testify/require"
	kscrape "tkestack.io/kvass/pkg/scrape"
)

func TestScrapeStatus_SetScrapeErr(t *testing.T) {
	r := require.New(t)
	st := NewScrapeStatus(10, 10)
	r.Equal(int64(10), st.Series)
	st.SetScrapeErr(time.Now(), nil)
	r.Equal(scrape.HealthGood, st.Health)
	r.Equal("", st.LastError)

	st = NewScrapeStatus(10, 10)
	st.SetScrapeErr(time.Now(), fmt.Errorf("test"))
	r.Equal(scrape.HealthBad, st.Health)
	r.Equal("test", st.LastError)
}

func TestScrapeStatus_UpdateSamples(t *testing.T) {
	r := require.New(t)
	st := NewScrapeStatus(1, 1)
	rs := kscrape.NewStatisticsSeriesResult()
	rs.ScrapedTotal = 2
	st.UpdateScrapeResult(rs)
	st.UpdateScrapeResult(rs)
	st.UpdateScrapeResult(rs)
	st.UpdateScrapeResult(rs)
	r.Equal(int64(2), st.Series)
}
