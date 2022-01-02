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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMustJSON(t *testing.T) {
	var a = struct {
		A string
	}{
		A: "test",
	}

	require.Equal(t, `{"A":"test"}`, MustJSON(&a))
	require.Equal(t, "", MustJSON(nil))
}

func TestMustYAMLV2(t *testing.T) {
	var a = struct {
		A string
	}{
		A: "test",
	}

	require.Equal(t, `a: test
`, MustYAMLV2(&a))
}

func TestCopyJSON(t *testing.T) {
	from := "test"
	dst := ""
	require.NoError(t, CopyJSON(&dst, &from))
	require.Equal(t, "test", dst)
	require.Error(t, CopyJSON(dst, from))
	require.Error(t, CopyJSON(dst, nil))
}
