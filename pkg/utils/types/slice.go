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

package types

import "strings"

// FindString return true if target in slice, return false if not
func FindString(target string, slice ...string) bool {
	for _, str := range slice {
		if str == target {
			return true
		}
	}
	return false
}

// FindString return true if target in slice, return false if not
func FindStringVague(target string, slice ...string) bool {
	for _, str := range slice {
		if strings.Contains(str, target) {
			return true
		}
	}
	return false
}
