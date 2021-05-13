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

package test

import (
	"encoding/json"
	"gopkg.in/yaml.v2"
)

// MustJSON marshal obj to json string, a panic will be thrown if marshal failed
func MustJSON(obj interface{}) string {
	if obj == nil {
		return ""
	}
	data, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// MustYAMLV2 marshal obj to yaml string, a panic will be thrown if marshal failed
func MustYAMLV2(obj interface{}) string {
	data, err := yaml.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// CopyJSON copy object via json marshal
func CopyJSON(dst, from interface{}) error {
	data, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
