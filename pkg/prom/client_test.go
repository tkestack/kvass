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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func dataServer(data string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(data))
	}))
}

func TestClient_TSDBInfo(t *testing.T) {
	w := dataServer(`{
  "status": "success",
  "data": {
    "headStats": {
       "numSeries": 508
     }
    }
}`)
	defer w.Close()
	c := NewClient(w.URL)
	r, err := c.TSDBInfo()
	require.NoError(t, err)
	require.Equal(t, int64(508), r.HeadStats.NumSeries)
}

func TestClient_ConfigReload(t *testing.T) {
	w := dataServer(``)
	defer w.Close()
	c := NewClient(w.URL)
	err := c.ConfigReload()
	require.NoError(t, err)
}

func TestClient_Targets(t *testing.T) {
	w := dataServer(`{
  "status": "success",
  "data": {
    "activeTargets": [
      {
        "discoveredLabels": {
          "__address__": "127.0.0.1:9090",
          "__metrics_path__": "/metrics",
          "__scheme__": "http",
          "job": "prometheus"
        },
        "labels": {
          "instance": "127.0.0.1:9090",
          "job": "prometheus"
        },
        "scrapePool": "prometheus",
        "scrapeUrl": "http://127.0.0.1:9090/metrics",
        "lastError": "",
        "lastScrape": "2017-01-17T15:07:44.723715405+01:00",
        "lastScrapeDuration": 0.050688943,
        "health": "up"
      }
    ],
    "droppedTargets": [
      {
        "discoveredLabels": {
          "__address__": "127.0.0.1:9100",
          "__metrics_path__": "/metrics",
          "__scheme__": "http",
          "job": "node"
        }
      }
    ]
  }
}`)
	defer w.Close()
	c := NewClient(w.URL)
	tar, err := c.Targets("all")
	require.NoError(t, err)
	require.Equal(t, 1, len(tar.ActiveTargets))
	require.Equal(t, 1, len(tar.DroppedTargets))
}
