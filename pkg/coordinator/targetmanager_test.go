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

package coordinator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"tkestack.io/kvass/pkg/prom"

	"github.com/stretchr/testify/require"

	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/shard"
)

func TestTargetManager_Update(t *testing.T) {
	tm := NewDefTargetManager(1, logrus.New())
	st := &shard.Target{
		JobName: "test",
		URL:     "http://127.0.0.1",
		Samples: -1,
	}

	tm.Update([]*shard.Target{st})
	ct := tm.Get(st.Hash())
	require.NotNil(t, ct)
	select {
	case <-tm.needExplore:
	default:
		t.Fatalf("target need explore")
	}
}

func TestTargetManager_Run(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	tm := NewDefTargetManager(1, logrus.New())
	tm.retryInterval = time.Duration(0)
	tm.ScrapeInfos = prom.NewScrapInfos(map[string]*prom.ScrapeInfo{
		"test": {},
	})

	count := 0
	tm.explore = func(scrapeInfo *prom.ScrapeInfo, url string) (i int64, e error) {
		count++
		if count == 1 {
			return 0, fmt.Errorf("test")
		}
		if count == MaxExploreTime+2 {
			cancel()
		}
		return 100, nil
	}

	tar := &shard.Target{
		JobName: "test",
		Samples: -1,
	}
	tm.Update([]*shard.Target{tar})

	r.NoError(tm.Run(ctx))
	r.Equal(int64(100), tar.Samples)
}
