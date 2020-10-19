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
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/prometheus/common/model"

	"github.com/stretchr/testify/require"

	config_util "github.com/prometheus/common/config"

	"github.com/prometheus/prometheus/config"
)

func TestNewScrapeInfos(t *testing.T) {
	r := require.New(t)
	cfg := &config.ScrapeConfig{
		JobName: "test",
	}
	u, _ := url.Parse("http://127.0.0.1:8008")
	cfg.HTTPClientConfig.ProxyURL = config_util.URL{URL: u}
	cfg.ScrapeTimeout = model.Duration(time.Second)
	r.NoError(os.Setenv("SCRAPE_PROXY", "http://127.0.0.1:9090"))

	ss := &ScrapeInfos{}
	r.NoError(ss.ApplyConfig(&config.Config{ScrapeConfigs: []*config.ScrapeConfig{cfg}}))
	s := ss.Get(cfg.JobName)
	r.NotNil(s)

	r.Equal(time.Second, s.timeout)
	r.Equal(u.String(), s.proxyUrl.String())
}

func getGzipData(data []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(data)
	zw.Flush()
	zw.Close()
	return buf.Bytes()
}

func TestScrape(t *testing.T) {
	data := []byte(`metrics0{code="200"} 1
metrics0{code="201"} 2
`)
	var cases = []struct {
		name     string
		dataType string
		data     []byte
	}{
		{
			name:     "no gzip",
			dataType: "",
			data:     data,
		},
		{
			name:     "gzip",
			dataType: "gzip",
			data:     getGzipData(data),
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				r.Equal("http://127.0.0.1:8080", req.Header["Origin-Proxy"][0])
				w.Header().Set("Content-Encoding", cs.dataType)
				w.Header().Set("Content-Type", "application/openmetrics-text")
				w.Write(cs.data)
			}))
			defer ts.Close()

			u, _ := url.Parse("http://127.0.0.1:8080")
			info := &ScrapeInfo{
				Client:   ts.Client(),
				Config:   &config.ScrapeConfig{JobName: "test"},
				proxyUrl: u,
			}

			retData, typ, err := Scrape(info, ts.URL)
			r.NoError(err)
			r.Equal(string(data), string(retData))
			r.Equal("application/openmetrics-text", typ)
		})
	}
}

func TestStatisticSample(t *testing.T) {
	data := `metrics0{code="200"} 1
metrics0{code="201"} 2
`
	r, err := relabel.NewRegexp("200")
	require.NoError(t, err)
	s, err := StatisticSample([]byte(data), "", []*relabel.Config{
		{
			SourceLabels: []model.LabelName{"code"},
			Regex:        r,
			Action:       relabel.Drop,
		},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), s)
}
