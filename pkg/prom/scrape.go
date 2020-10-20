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

package prom

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/prometheus/prometheus/pkg/textparse"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/version"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/config"
)

const acceptHeader = `application/openmetrics-text; version=0.0.1,text/plain;version=0.0.4;q=0.5,*/*;q=0.1`

var userAgentHeader = fmt.Sprintf("PrometheusURL/%s", version.Version)

// ScrapeInfo container http client for scraping target
type ScrapeInfo struct {
	// Client is the http.Client for scraping
	// all scraping request will be proxy to env SCRAPE_PROXY if it is not empty
	Client *http.Client
	// Config is the origin scrape config in config file
	Config *config.ScrapeConfig
	// proxyURL save old proxyURL set in ScrapeConfig if env SCRAPE_PROXY is not empty
	// proxyURL will be saved in head "Origin-Proxy" when scrape request is send
	proxyURL *url.URL
	timeout  time.Duration
}

func newScrapeInfo(cfg *config.ScrapeConfig) (*ScrapeInfo, error) {
	proxy := os.Getenv("SCRAPE_PROXY")
	oldProxy := cfg.HTTPClientConfig.ProxyURL
	if proxy != "" {
		newURL, err := url.Parse(proxy)
		if err != nil {
			return nil, errors.Wrapf(err, "proxy parse failed")
		}
		cfg.HTTPClientConfig.ProxyURL.URL = newURL
	}

	client, err := config_util.NewClientFromConfig(cfg.HTTPClientConfig, cfg.JobName, true)
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP Client")
	}

	return &ScrapeInfo{
		Client:   client,
		Config:   cfg,
		proxyURL: oldProxy.URL,
		timeout:  time.Duration(cfg.ScrapeTimeout),
	}, nil
}

// ScrapeInfos includes all ScrapeInfo of all jobs
type ScrapeInfos struct {
	// key is job name
	m map[string]*ScrapeInfo
}

// NewScrapInfos create a ScrapeInfos with specified ScrapeInfo set
func NewScrapInfos(m map[string]*ScrapeInfo) *ScrapeInfos {
	return &ScrapeInfos{m: m}
}

// ApplyConfig update ScrapeInfos from config
func (s *ScrapeInfos) ApplyConfig(cfg *config.Config) error {
	ret := map[string]*ScrapeInfo{}
	for _, cfg := range cfg.ScrapeConfigs {
		info, err := newScrapeInfo(cfg)
		if err != nil {
			return errors.Wrap(err, cfg.JobName)
		}
		ret[cfg.JobName] = info
	}
	s.m = ret
	return nil
}

// Get search ScrapeInfo by job name, nil will be return if job not exist
func (s *ScrapeInfos) Get(job string) *ScrapeInfo {
	return s.m[job]
}

// Scrape scrape a url and return origin metrics data and contentType
func Scrape(s *ScrapeInfo, url string) ([]byte, string, error) {
	buf := bytes.NewBuffer(make([]byte, 0))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Add("Accept", acceptHeader)
	req.Header.Add("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", userAgentHeader)
	req.Header.Set("X-PrometheusURL-Scrape-Timeout-Seconds", fmt.Sprintf("%f", s.timeout.Seconds()))
	if s.proxyURL != nil {
		req.Header.Set("Origin-Proxy", s.proxyURL.String())
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, "", errors.Errorf("server returned HTTP status %s", resp.Status)
	}

	if resp.Header.Get("Content-Encoding") != "gzip" {
		_, err = io.Copy(buf, resp.Body)
		if err != nil {
			return nil, "", err
		}
		return buf.Bytes(), resp.Header.Get("Content-Type"), nil
	}

	gzipr, err := gzip.NewReader(bufio.NewReader(resp.Body))
	if err != nil {
		return nil, "", err
	}

	_, err = io.Copy(buf, gzipr)
	gzipr.Close()
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), resp.Header.Get("Content-Type"), nil
}

// StatisticSample statistic samples from bytes
func StatisticSample(b []byte, contentType string, rc []*relabel.Config) (total int64, err error) {
	var (
		p  = textparse.New(b, contentType)
		et textparse.Entry
	)
	for {
		if et, err = p.Next(); err != nil {
			if err == io.EOF {
				err = nil
			}
			return total, err
		}

		switch et {
		case textparse.EntrySeries:
			var lset labels.Labels
			_ = p.Metric(&lset)
			if relabel.Process(lset, rc...) != nil {
				total++
			}
		}
	}
}
