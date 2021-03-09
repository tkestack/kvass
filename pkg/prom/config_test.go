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

package prom

import (
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"testing"
)

func TestConfigManager(t *testing.T) {
	r := require.New(t)
	file := t.TempDir() + "config.yaml"
	data := `global:
  evaluation_interval: 10s
  scrape_interval: 15s
  external_labels:
    replica: pod1
scrape_configs:
- job_name: "test"
  static_configs:
  - targets:
    - 127.0.0.1:9091`

	r.NoError(ioutil.WriteFile(file, []byte(data), 0777))
	m := NewConfigManager(file, logrus.New())
	updated := false
	m.AddReloadCallbacks(func(c *ConfigInfo) error {
		updated = true
		r.Equal(string(c.RawContent), data)
		r.Equal("16887931695534343218", c.ConfigHash)
		r.Equal(1, len(c.Config.ScrapeConfigs))
		return nil
	})
	r.NoError(m.Reload())
	r.NotNil(m.ConfigInfo())
	r.True(updated)
}
