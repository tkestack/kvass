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
	"tkestack.io/kvass/pkg/shard"
)

type shardManager struct {
	shards []shardConfig
	log    logrus.FieldLogger
}

func newShardManager(shards []shardConfig, log logrus.FieldLogger) *shardManager {
	return &shardManager{
		shards: shards,
		log:    log,
	}
}

// Shards return current Shards in the cluster
func (s *shardManager) Shards() ([]*shard.Shard, error) {
	ret := make([]*shard.Shard, 0)
	for _, sd := range s.shards {
		ret = append(ret, shard.NewShard(sd.ID, sd.URL, true, s.log.WithField("shard", sd.ID)))
	}
	return ret, nil
}

// ChangeScale create or delete Shards according to "expReplicate"
// static shard can not change scale
func (s *shardManager) ChangeScale(expReplicate int32) error {
	return nil
}
