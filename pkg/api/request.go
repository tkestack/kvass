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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/pkg/errors"
)

func Post(url string, req interface{}, ret interface{}) (err error) {
	reqData := make([]byte, 0)
	if req != nil {
		reqData, err = json.Marshal(req)
		if err != nil {
			return err
		}
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(reqData))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return dealResp(resp, ret)
}

// Get do get request to target url and save data to ret
func Get(url string, ret interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return errors.Wrapf(err, "get runtimeinfo from prometheus")
	}
	defer func() { _ = resp.Body.Close() }()
	return dealResp(resp, ret)
}

func dealResp(resp *http.Response, ret interface{}) error {
	if resp.StatusCode != 200 {
		return fmt.Errorf("status code is %d", resp.StatusCode)
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrapf(err, "read data")
	}

	if ret != nil {
		commonResp := Data(ret)
		if err := json.Unmarshal(data, commonResp); err != nil {
			return errors.Wrapf(err, "Unmarshal")
		}

		if commonResp.Status != StatusSuccess {
			return fmt.Errorf(commonResp.Err)
		}
	}

	return nil
}
