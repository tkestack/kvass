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
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestCall create a httptest server and do http request to it
// the data in params will be write to server and the ret in params is deemed to the Data of common Result
func TestCall(t *testing.T, serveHTTP func(w http.ResponseWriter, req *http.Request), uri, method, data string, ret interface{}) (*require.Assertions, *Result) {
	gin.SetMode(gin.ReleaseMode)
	req := httptest.NewRequest(method, uri, strings.NewReader(data))
	w := httptest.NewRecorder()

	serveHTTP(w, req)

	result := w.Result()
	defer result.Body.Close()
	r := require.New(t)
	body, err := ioutil.ReadAll(result.Body)
	r.NoError(err)
	resObj := &Result{Data: ret}
	if len(body) != 0 {
		_ = json.Unmarshal(body, resObj)
	}
	if ret != nil {
		r.Equal(StatusSuccess, resObj.Status)
		r.Empty(resObj.Err)
		r.Empty(resObj.ErrorType)
	}
	return r, resObj
}
