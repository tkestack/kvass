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

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindString(t *testing.T) {
	require.True(t, FindString("1", "1", "2"))
	require.False(t, FindString("3", "1", "2"))
}

func TestFindStringVague(t *testing.T) {
	require.True(t, FindStringVague("1", "1", "2"))
	require.True(t, FindStringVague("1", "11", "22"))
	require.True(t, FindStringVague("api/v1/shard/runtimeinfo", "/api/v1/shard/runtimeinfo/", "22"))
	require.False(t, FindStringVague("3", "1", "2"))
}
