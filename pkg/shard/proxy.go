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
	"bytes"
	"fmt"
	"io"
	"net/http"

	"tkestack.io/kvass/pkg/prom"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// emptyMetrics is a Handler return empty metircs
var emptyMetrics = promhttp.HandlerFor(
	prometheus.NewRegistry(),
	promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	},
)

// Proxy is a Proxy server for prometheus tManager
// Proxy will return an empty metrics if this target if not allowed to scrape for this prometheus client
// otherwise, Proxy do real tManager, statistic metrics samples and return metrics to prometheus
type Proxy struct {
	*prom.ScrapeInfos
	scraping *TargetManager
	log      logrus.FieldLogger
}

// NewProxy create a new proxy server
func NewProxy(scraping *TargetManager, log logrus.FieldLogger) *Proxy {
	return &Proxy{
		ScrapeInfos: prom.NewScrapInfos(nil),
		scraping:    scraping,
		log:         log,
	}
}

// Run start Proxy server and block
func (p *Proxy) Run(address string) error {
	return http.ListenAndServe(address, p)
}

// ServeHTTP handle one Proxy request
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	job := r.FormValue(jobNameFormName)
	r.Form.Del(jobNameFormName)
	jobInfo := p.ScrapeInfos.Get(job)
	if jobInfo == nil {
		p.log.Errorf("can not found job client of %s", job)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	r.URL.RawQuery = r.Form.Encode()
	r.URL.Scheme = jobInfo.Config.Scheme

	tt := p.scraping.Get(job, r.URL.String())
	// target not found, skip this target
	if tt == nil {
		p.log.Debugf("unknown target %s/%s", job, r.URL.String())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// target not assign to this shard
	if !tt.scraping {
		// according to target health info of this target
		// we return empty metrics or return an error to prometheus
		if tt.Healthy == "up" {
			emptyMetrics.ServeHTTP(w, r)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		return
	}

	var scrapErr error
	defer func() {
		if scrapErr != nil {
			tt.setLastErr(scrapErr.Error())
			w.WriteHeader(http.StatusInternalServerError)
			p.log.Error(scrapErr.Error())
		} else {
			tt.setLastErr("")
		}
	}()

	// real scraping
	data, contentType, err := prom.Scrape(jobInfo, r.URL.String())
	if err != nil {
		scrapErr = fmt.Errorf("get data %v", err)
		return
	}

	// send origin result to prometheus
	if _, err := io.Copy(w, bytes.NewBuffer(data)); err != nil {
		scrapErr = fmt.Errorf("copy data to prometheus failed %v", err)
		return
	}

	samples, err := prom.StatisticSample(data, contentType, jobInfo.Config.MetricRelabelConfigs)
	if err != nil {
		scrapErr = fmt.Errorf("statisticSample failed %v", err)
		return
	}

	p.log.Debugf("target sample is %d %s", samples, tt.Hash())
	tt.UpdateSamples(samples)
}
