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
	"fmt"
	"testing"

	v1 "github.com/prometheus/prometheus/web/api/v1"

	"github.com/prometheus/prometheus/config"

	"github.com/stretchr/testify/require"
)

func TestTarget_UpdateSamples(t *testing.T) {
	tar := Target{}
	tar.UpdateSamples(1)
	require.Equal(t, int64(1), tar.Samples)
	tar.UpdateSamples(2)
	tar.UpdateSamples(2)
	tar.UpdateSamples(2)
	require.Equal(t, int64(2), tar.Samples)
}

func TestTargetFromProm(t *testing.T) {
	tar := &v1.Target{
		ScrapePool: "test",
		ScrapeURL:  fmt.Sprintf("http://127.0.0.1:9090?%s=%s", jobNameFormName, "test"),
		Health:     "up",
	}

	cfg := &config.ScrapeConfig{
		JobName: "test",
		Scheme:  "https",
	}

	localT := TargetFromProm(cfg, tar)
	require.Equal(t, tar.ScrapePool, localT.JobName)
	require.Equal(t, "https://127.0.0.1:9090", localT.URL)
	require.Equal(t, "up", localT.Healthy)
	require.Equal(t, int64(-1), localT.Samples)
}
