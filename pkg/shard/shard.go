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
	"net/url"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/target"
)

// Shard is a prometheus shard
type Shard struct {
	// ID is the unique ID for differentiate different replicate of shard
	ID string
	// APIGet is a function to do stand api request to target
	// exposed this field for user to writ unit testing easily
	APIGet func(url string, ret interface{}) error
	// APIPost is a function to do stand api request to target
	// exposed this field for user to writ unit testing easily
	APIPost func(url string, req interface{}, ret interface{}) (err error)
	// scraping is the cached ScrapeStatus fetched from sidecar last time
	scraping map[uint64]*target.ScrapeStatus
	url      string
	log      logrus.FieldLogger
	// Ready indicate this shard is ready
	Ready bool
}

// NewShard create a Shard with empty scraping cache
func NewShard(id string, url string, ready bool, log logrus.FieldLogger) *Shard {
	return &Shard{
		ID:      id,
		APIGet:  api.Get,
		APIPost: api.Post,
		url:     url,
		log:     log,
		Ready:   ready,
	}
}

//Samples return the sample statistics of last scrape
func (r *Shard) Samples(jobName string, withMetricsDetail bool) (map[string]*scrape.StatisticsSeriesResult, error) {
	ret := map[string]*scrape.StatisticsSeriesResult{}
	param := url.Values{}
	if jobName != "" {
		param["job"] = []string{jobName}
	}
	if withMetricsDetail {
		param["with_metrics_detail"] = []string{"true"}
	}

	u := r.url + "/api/v1/shard/samples/"
	if len(param) != 0 {
		u += "?" + param.Encode()
	}

	err := r.APIGet(u, &ret)
	if err != nil {
		return nil, fmt.Errorf("get samples info from %s failed : %s", r.ID, err.Error())
	}

	return ret, nil
}

// RuntimeInfo return the runtime status of this shard
func (r *Shard) RuntimeInfo() (*RuntimeInfo, error) {
	res := &RuntimeInfo{}
	err := r.APIGet(r.url+"/api/v1/shard/runtimeinfo/", &res)
	if err != nil {
		return res, fmt.Errorf("get runtime info from %s failed : %s", r.ID, err.Error())
	}

	return res, nil
}

// TargetStatus return the target runtime status that Group scraping
// cached result will be send if something wrong
func (r *Shard) TargetStatus() (map[uint64]*target.ScrapeStatus, error) {
	res := map[uint64]*target.ScrapeStatus{}

	err := r.APIGet(r.url+"/api/v1/shard/targets/status/", &res)
	if err != nil {
		return res, errors.Wrapf(err, "get targets status info from %s failed, url = %s", r.ID, r.url)
	}

	//must copy
	m := map[uint64]*target.ScrapeStatus{}
	for k, v := range res {
		newV := *v
		m[k] = &newV
	}

	r.scraping = m
	return res, nil
}

// UpdateConfig try update shard config by API
func (r *Shard) UpdateConfig(req *UpdateConfigRequest) error {
	return r.APIPost(r.url+"/api/v1/status/config", req, nil)
}

// UpdateTarget try apply targets to sidecar
// request will be skipped if nothing changed according to r.scraping
func (r *Shard) UpdateTarget(request *UpdateTargetsRequest) error {
	newTargets := map[uint64]*target.Target{}
	for _, ts := range request.Targets {
		for _, t := range ts {
			newTargets[t.Hash] = t
		}
	}

	if r.needUpdate(newTargets) {
		if len(newTargets) != 0 || len(r.scraping) != 0 {
			r.log.Infof("%s need update targets", r.ID)
		}
		if err := r.APIPost(r.url+"/api/v1/shard/targets/", &request, nil); err != nil {
			return err
		}
	}

	return nil
}

func (r *Shard) needUpdate(targets map[uint64]*target.Target) bool {
	if len(targets) != len(r.scraping) || len(targets) == 0 {
		return true
	}

	for k, v := range targets {
		if r.scraping[k] == nil || r.scraping[k].TargetState != v.TargetState {
			return true
		}
	}
	return false
}
