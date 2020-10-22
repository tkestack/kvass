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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sirupsen/logrus"
)

func TestTargetManager(t *testing.T) {
	scraping := &Target{
		JobName: "test",
		URL:     "t0",
	}

	noScraping := &Target{
		JobName: "test",
		URL:     "t1",
	}

	targets := map[string][]*Target{
		"shard-0": {scraping},
		IDUnknown: {noScraping},
	}

	r := require.New(t)
	tm := NewTargetManager(logrus.New())
	tm.Update(targets, "shard-0")
	r.Empty(tm.Get("notExist", ""))

	tar := tm.Get(scraping.JobName, scraping.URL)
	r.True(tar.scraping)
	r.Equal("shard-0", tar.shardID)

	tar = tm.Get(noScraping.JobName, noScraping.URL)
	r.False(tar.scraping)
	r.Equal(IDUnknown, tar.shardID)
}
