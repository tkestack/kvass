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

package scrape

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/version"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/config"
)

const acceptHeader = `application/openmetrics-text; version=0.0.1,text/plain;version=0.0.4;q=0.5,*/*;q=0.1`

var userAgentHeader = fmt.Sprintf("prometheusURL/%s", version.Version)

// JobInfo contains http client for scraping target, and the origin scrape config
type JobInfo struct {
	// Config is the origin scrape config in config file
	Config *config.ScrapeConfig
	// Cli is the http.Cli for scraping
	// all scraping request will be proxy to env SCRAPE_PROXY if it is not empty
	Cli *http.Client
	// proxyURL save old proxyURL set in ScrapeConfig if env SCRAPE_PROXY is not empty
	// proxyURL will be saved in head "Origin-Proxy" when scrape request is send
	proxyURL *url.URL
}

func newJobInfo(cfg config.ScrapeConfig, keeAliveDisable bool) (*JobInfo, error) {
	proxy := os.Getenv("SCRAPE_PROXY")
	oldProxy := cfg.HTTPClientConfig.ProxyURL
	if proxy != "" {
		newURL, err := url.Parse(proxy)
		if err != nil {
			return nil, errors.Wrapf(err, "proxy parse failed")
		}
		cfg.HTTPClientConfig.ProxyURL.URL = newURL
	}

	option := make([]config_util.HTTPClientOption, 0)
	if keeAliveDisable {
		option = append(option, config_util.WithKeepAlivesDisabled())
	}

	client, err := config_util.NewClientFromConfig(cfg.HTTPClientConfig, cfg.JobName, option...)
	if err != nil {
		return nil, errors.Wrap(err, "error creating HTTP Cli")
	}

	return &JobInfo{
		Cli:      client,
		Config:   &cfg,
		proxyURL: oldProxy.URL,
	}, nil
}
