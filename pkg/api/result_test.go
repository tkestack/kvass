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
	"fmt"
	"io/ioutil"
	"net/http/httptest"
	"testing"

	"tkestack.io/kvass/pkg/utils/test"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/stretchr/testify/require"
)

func TestData(t *testing.T) {
	s := Data(map[string]string{})
	require.NotNil(t, s.Data)
	require.Empty(t, s.ErrorType)
	require.Empty(t, s.Err)
	require.Equal(t, StatusSuccess, s.Status)
}

func TestInternalErr(t *testing.T) {
	s := InternalErr(fmt.Errorf("1"), "test")
	require.Nil(t, s.Data)
	require.Equal(t, ErrorInternal, s.ErrorType)
	require.NotEmpty(t, s.Err)
	require.Equal(t, StatusError, s.Status)
}

func TestBadDataErr(t *testing.T) {
	s := BadDataErr(fmt.Errorf("1"), "test")
	require.Nil(t, s.Data)
	require.Equal(t, ErrorBadData, s.ErrorType)
	require.NotEmpty(t, s.Err)
	require.Equal(t, StatusError, s.Status)
}

func TestWrap(t *testing.T) {
	var cases = []struct {
		name   string
		code   int
		result *Result
	}{
		{
			name:   "test that return interval error",
			code:   503,
			result: InternalErr(fmt.Errorf(""), "test"),
		},
		{
			name:   "test that return bad request error",
			code:   400,
			result: BadDataErr(fmt.Errorf(""), "test"),
		},
		{
			name:   "test that return success",
			code:   200,
			result: Data(map[string]string{}),
		},
		{
			name:   "test that return empty",
			code:   200,
			result: nil,
		},
	}

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			r := require.New(t)
			e := gin.Default()
			e.GET("/test", Wrap(logrus.New(), func(ctx *gin.Context) *Result {
				return cs.result
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			result := w.Result()

			body, err := ioutil.ReadAll(result.Body)
			r.NoError(err)
			r.Equal(cs.code, result.StatusCode)

			if cs.result == nil {
				r.Empty(body)
				return
			}
			r.JSONEq(test.MustJson(cs.result), string(body))
		})
	}
}
