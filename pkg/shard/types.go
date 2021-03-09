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
	"time"
	"tkestack.io/kvass/pkg/target"
)

// ReplicasManager known all shard managers
type ReplicasManager interface {
	// Replicas return all replicas
	Replicas() ([]Manager, error)
}

// Manager known how to create or delete Shards
type Manager interface {
	// Shards return current Shards in the cluster
	Shards() ([]*Shard, error)
	// ChangeScale create or delete Shards according to "expReplicate"
	ChangeScale(expReplicate int32) error
}

// RuntimeInfo contains all running status of this shard
type RuntimeInfo struct {
	// HeadSeries return current head_series of prometheus
	HeadSeries int64 `json:"headSeries"`
	// ConfigMD5 is the md5 of current config file
	ConfigMD5 string `json:"ConfigMD5"`
	// IdleStartAt is the time that shard begin idle
	IdleStartAt *time.Time `json:"IdleStartAt,omitempty"`
}

// UpdateTargetsRequest contains all information about the targets updating request
type UpdateTargetsRequest struct {
	// targets contains all targets this shard should scrape
	Targets map[string][]*target.Target
}
