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
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"tkestack.io/kvass/pkg/utils/test"

	"tkestack.io/kvass/pkg/api"
)

func dataServer(ret interface{}) *httptest.Server {
	data, err := json.Marshal(ret)
	if err != nil {
		panic(err)
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write(data)
	}))
}

func TestClient_RuntimeInfo(t *testing.T) {
	want := &RuntimeInfo{
		ID:         "shard-0",
		HeadSeries: 10,
		Targets: map[string][]*Target{
			IDUnknown: {{
				JobName: "test",
			}},
		},
	}
	ts := dataServer(api.Data(want))
	defer ts.Close()

	c := NewClient(ts.URL, time.Second)
	r, err := c.RuntimeInfo()
	require.NoError(t, err)
	require.JSONEq(t, test.MustJSON(want), test.MustJSON(r))
}

func TestClient_UpdateRuntimeInfo(t *testing.T) {
	want := &RuntimeInfo{
		ID:         "shard-0",
		HeadSeries: 10,
		Targets: map[string][]*Target{
			IDUnknown: {{
				JobName: "test",
			}},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, err := ioutil.ReadAll(req.Body)
		require.NoError(t, err)
		require.JSONEq(t, test.MustJSON(want), string(data))
	}))
	defer ts.Close()
	c := NewClient(ts.URL, time.Second)
	require.NoError(t, c.UpdateRuntimeInfo(want))
}
