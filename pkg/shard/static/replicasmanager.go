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
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"tkestack.io/kvass/pkg/shard"
)

// ReplicasManager create replicas from static
type ReplicasManager struct {
	file string
	log  logrus.FieldLogger
}

// NewReplicasManager return an new static shard replicas manager
func NewReplicasManager(file string, log logrus.FieldLogger) *ReplicasManager {
	return &ReplicasManager{
		file: file,
		log:  log,
	}
}

// Replicas return all shards manager
func (g *ReplicasManager) Replicas() ([]shard.Manager, error) {
	content, err := ioutil.ReadFile(g.file)
	if err != nil {
		return nil, errors.Wrapf(err, "read config file")
	}
	config := &staticConfig{}
	if err := yaml.Unmarshal(content, config); err != nil {
		return nil, errors.Wrapf(err, "wrong format of shard config")
	}

	ret := make([]shard.Manager, 0)
	for _, r := range config.Replicas {
		ret = append(ret, newShardManager(r.Shards, g.log))
	}

	return ret, nil
}
