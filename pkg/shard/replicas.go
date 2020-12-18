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
	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/target"
)

// Replicas is a replicas of one shard
// all replicas of one shard scrape same targets and expected to have same load
type Replicas struct {
	// ID is the unique ID for differentiate different replicate of shard
	ID string
	// APIGet is a function to do stand api request to target
	// exposed this field for user to writ unit testing easily
	APIGet func(url string, ret interface{}) error
	// APIPost is a function to do stand api request to target
	// exposed this field for user to writ unit testing easily
	APIPost func(url string, req interface{}, ret interface{}) (err error)
	// scraping is a map that record the targets this Replicas scarping
	// the key is target hash, scraping cache is invalid if it is nil
	scraping map[uint64]bool
	url      string
	log      logrus.FieldLogger
}

// NewReplicas create a Replicas with empty scraping cache
func NewReplicas(id string, url string, log logrus.FieldLogger) *Replicas {
	return &Replicas{
		ID:      id,
		APIGet:  api.Get,
		APIPost: api.Post,
		url:     url,
		log:     log,
	}
}

func (r *Replicas) targetsScraping() (map[uint64]bool, error) {
	if r.scraping == nil {
		res, err := r.targetStatus()
		if err != nil {
			return nil, err
		}
		c := map[uint64]bool{}
		for k := range res {
			c[k] = true
		}
		r.scraping = c
	}
	return r.scraping, nil
}

func (r *Replicas) runtimeInfo() (*RuntimeInfo, error) {
	res := &RuntimeInfo{}

	err := r.APIGet(r.url+"/api/v1/shard/runtimeinfo/", &res)
	if err != nil {
		return nil, fmt.Errorf("get runtime info from %s failed : %s", r.ID, err.Error())
	}

	return res, nil
}

func (r *Replicas) targetStatus() (map[uint64]*target.ScrapeStatus, error) {
	res := map[uint64]*target.ScrapeStatus{}

	err := r.APIGet(r.url+"/api/v1/shard/targets/", &res)
	if err != nil {
		return nil, fmt.Errorf("get targets status info from %s failed : %s", r.ID, err.Error())
	}

	return res, nil
}

func (r *Replicas) updateTarget(request *UpdateTargetsRequest) error {
	newCache := map[uint64]bool{}
	for _, ts := range request.Targets {
		for _, t := range ts {
			newCache[t.Hash] = true
		}
	}

	if r.needUpdate(newCache) {
		r.log.Infof("%s need update targets", r.ID)
		if err := r.APIPost(r.url+"/api/v1/shard/targets/", &request, nil); err != nil {
			return err
		}
		r.scraping = newCache
	}

	return nil
}

func (r *Replicas) needUpdate(cache map[uint64]bool) bool {
	if len(cache) != len(r.scraping) {
		return true
	}

	for k, v := range cache {
		if r.scraping[k] != v {
			return true
		}
	}
	return false
}
