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

package static

import (
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestShardManager_Shards(t *testing.T) {
	shards := []shardConfig{
		{
			ID:  "0",
			URL: "http://1.1.1.1",
		},
	}
	m := newShardManager(shards, logrus.New())
	sd, err := m.Shards()
	require.NoError(t, err)
	require.Equal(t, 1, len(sd))
	require.Equal(t, shards[0].ID, sd[0].ID)
}

func TestShardManager_ChangeScale(t *testing.T) {
	m := newShardManager(nil, logrus.New())
	require.NoError(t, m.ChangeScale(0))
}
