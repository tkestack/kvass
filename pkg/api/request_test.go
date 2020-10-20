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

package api

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"tkestack.io/kvass/pkg/utils/test"

	"tkestack.io/kvass/pkg/utils/types"
)

type caseType struct {
	name       string
	statusCode int
	req        interface{}
	ret        interface{}
	respData   string
	wantRet    interface{}
	wantErr    bool
}

var cases = []caseType{
	{
		name:       "return no 200 status code",
		statusCode: 503,
		wantErr:    true,
		ret:        types.StringPtr(""),
		wantRet:    types.StringPtr("test"),
	},
	{
		name:       "return error response",
		statusCode: 200,
		respData: `
{
	"status":"error",
	"error":"test"
}`,
		wantErr: true,
		ret:     types.StringPtr(""),
		wantRet: types.StringPtr("test"),
	},
	{
		name:       "unknown resp format ",
		statusCode: 200,
		respData:   "---",
		ret:        types.StringPtr(""),
		wantRet:    types.StringPtr("test"),
		wantErr:    true,
	},
	{
		name:       "normal response",
		req:        1,
		statusCode: 200,
		respData: `
{
	"status":"success",
	"data":"test"
}
`,
		ret:     types.StringPtr(""),
		wantRet: types.StringPtr("test"),
		wantErr: false,
	},
}

func testCases(t *testing.T, method string) {
	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if !cs.wantErr && method == "POST" {
					data, err := ioutil.ReadAll(req.Body)
					r.NoError(err)
					r.Equal("1", string(data))
				}

				if cs.statusCode == 200 {
					w.Write([]byte(cs.respData))
				} else {
					w.WriteHeader(cs.statusCode)
				}
			}))
			defer ts.Close()

			var err error
			if method == "POST" {
				err = Post(ts.URL, cs.req, cs.ret)
			} else {
				err = Get(ts.URL, cs.ret)
			}

			if cs.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.JSONEq(test.MustJSON(cs.wantRet), test.MustJSON(cs.ret))
		})
	}
}

func TestGet(t *testing.T) {
	testCases(t, "GET")
}
func TestPost(t *testing.T) {
	testCases(t, "POST")
}
