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
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/target"

	"github.com/sirupsen/logrus"
)

// Proxy is a Proxy server for prometheus tManager
// Proxy will return an empty metrics if this target if not allowed to scrape for this prometheus client
// otherwise, Proxy do real tManager, statistic metrics samples and return metrics to prometheus
type Proxy struct {
	targetsLock sync.Mutex
	getJob      func(jobName string) *scrape.JobInfo
	getStatus   func() map[uint64]*target.ScrapeStatus
	log         logrus.FieldLogger
}

// NewProxy create a new proxy server
func NewProxy(
	getJob func(jobName string) *scrape.JobInfo,
	getStatus func() map[uint64]*target.ScrapeStatus,
	log logrus.FieldLogger) *Proxy {
	return &Proxy{
		getJob:    getJob,
		getStatus: getStatus,
		log:       log,
	}
}

// Run start Proxy server and block
func (p *Proxy) Run(address string) error {
	return http.ListenAndServe(address, p)
}

// ServeHTTP handle one Proxy request
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	job, hashStr, realURL := translateURL(*r.URL)
	jobInfo := p.getJob(job)
	if jobInfo == nil {
		p.log.Errorf("can not found job client of %s", job)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	hash, err := strconv.ParseUint(hashStr, 10, 64)
	if err != nil {
		p.log.Errorf("unexpected hash string %s", hashStr)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	tar := p.getStatus()[hash]

	start := time.Now()
	var scrapErr error
	defer func() {
		if tar != nil {
			tar.ScrapeTimes++
			tar.SetScrapeErr(start, scrapErr)
		}

		if scrapErr != nil {
			p.log.Error(err.Error())
			w.WriteHeader(http.StatusBadRequest)
		}
	}()

	data, contentType, err := jobInfo.Scrape(realURL.String())
	if err != nil {
		scrapErr = fmt.Errorf("get data %v", err)
		return
	}

	series, err := scrape.StatisticSeries(data, contentType, jobInfo.Config.MetricRelabelConfigs)
	if err != nil {
		scrapErr = fmt.Errorf("StatisticSeries failed %v", err)
		return
	}

	// send origin result to prometheus
	if _, err := io.Copy(w, bytes.NewBuffer(data)); err != nil {
		scrapErr = fmt.Errorf("copy data to prometheus failed %v", err)
		if time.Now().Sub(start) > time.Duration(jobInfo.Config.ScrapeTimeout) {
			scrapErr = fmt.Errorf("scrape timeout")
		}
		return
	}

	if tar != nil {
		tar.UpdateSeries(series)
	}
}

func translateURL(u url.URL) (job string, hash string, realURL url.URL) {
	vs := u.Query()
	job = vs.Get(paramJobName)
	hash = vs.Get(paramHash)
	scheme := vs.Get(paramScheme)

	vs.Del(paramHash)
	vs.Del(paramJobName)
	vs.Del(paramScheme)

	u.Scheme = scheme
	u.RawQuery = vs.Encode()
	return job, hash, u
}
