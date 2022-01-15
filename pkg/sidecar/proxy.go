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
	"net/http"
	"net/url"
	"strconv"
	"time"

	parser "github.com/VictoriaMetrics/VictoriaMetrics/lib/protoparser/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/target"
)

var (
	proxyTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kvass_sidecar_proxy_total",
	}, []string{})
	proxySeries = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kvass_sidecar_proxy_target_series",
	}, []string{"target_job", "url"})
	proxyScrapeDurtion = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kvass_sidecar_proxy_target_scrape_durtion",
	}, []string{"target_job", "url"})
)

// Proxy is a Proxy server for prometheus tManager
// Proxy will return an empty metrics if this target if not allowed to scrape for this prometheus client
// otherwise, Proxy do real tManager, statistic metrics samples and return metrics to prometheus
type Proxy struct {
	getJob    func(jobName string) *scrape.JobInfo
	getStatus func() map[uint64]*target.ScrapeStatus
	log       logrus.FieldLogger
}

// NewProxy create a new proxy server
func NewProxy(
	getJob func(jobName string) *scrape.JobInfo,
	getStatus func() map[uint64]*target.ScrapeStatus,
	promRegistry prometheus.Registerer,
	log logrus.FieldLogger) *Proxy {
	_ = promRegistry.Register(proxyTotal)
	_ = promRegistry.Register(proxySeries)
	_ = promRegistry.Register(proxyScrapeDurtion)
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
	proxyTotal.WithLabelValues().Inc()

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
			p.log.Errorf(scrapErr.Error())
			w.WriteHeader(http.StatusBadRequest)
		}
	}()

	scraper := scrape.NewScraper(jobInfo, realURL.String(), p.log)
	scraper.WithRawWriter(w)
	if err := scraper.RequestTo(); err != nil {
		scrapErr = fmt.Errorf("RequestTo %v", err)
		return
	}
	w.Header().Set("Content-Type", scraper.HTTPResponse.Header.Get("Content-Type"))

	series := int64(0)
	if err := scraper.ParseResponse(func(rows []parser.Row) error {
		series += scrape.StatisticSeries(rows, jobInfo.Config.MetricRelabelConfigs)
		return nil
	}); err != nil {
		scrapErr = fmt.Errorf("copy data to prometheus failed %v", err)
		if time.Since(start) > time.Duration(jobInfo.Config.ScrapeTimeout) {
			scrapErr = fmt.Errorf("scrape timeout")
		}
		return
	}
	proxySeries.WithLabelValues(jobInfo.Config.JobName, realURL.String()).Set(float64(series))
	proxyScrapeDurtion.WithLabelValues(jobInfo.Config.JobName, realURL.String()).Set(float64(time.Now().Sub(start)))
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
