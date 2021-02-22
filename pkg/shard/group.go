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
	"sync"
	"tkestack.io/kvass/pkg/target"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// Group is a shard group contains one or more replicates
// it knows how to communicate with shard sidecar and manager local scraping cache
type Group struct {
	// ID is the unique ID of this group
	ID         string
	replicates []*Replicas
	// scraping is the cached ScrapeStatus fetched last time
	scraping map[uint64]*target.ScrapeStatus
	log      logrus.FieldLogger
}

// NewGroup return a new shard with no replicate
func NewGroup(id string, lg logrus.FieldLogger) *Group {
	return &Group{
		ID:         id,
		log:        lg,
		replicates: []*Replicas{},
		scraping:   map[uint64]*target.ScrapeStatus{},
	}
}

// AddReplicas add a Replicas to this shard
func (s *Group) AddReplicas(r *Replicas) {
	s.replicates = append(s.replicates, r)
}

// Replicas return all replicates of this shard
func (s *Group) Replicas() []*Replicas {
	return s.replicates
}

// RuntimeInfo return the runtime status of this Group
func (s *Group) RuntimeInfo() (*RuntimeInfo, error) {
	ret := &RuntimeInfo{}
	l := sync.Mutex{}
	err := s.shardsDo(func(sd *Replicas) error {
		r, err := sd.runtimeInfo()
		if err != nil {
			return err
		}

		l.Lock()
		defer l.Unlock()

		if ret.HeadSeries < r.HeadSeries {
			ret = r
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// TargetStatus return the target runtime status that Group scraping
// cached result will be send if something wrong
func (s *Group) TargetStatus() (map[uint64]*target.ScrapeStatus, error) {
	ret := map[uint64]*target.ScrapeStatus{}
	l := sync.Mutex{}
	err := s.shardsDo(func(sd *Replicas) error {
		res, err := sd.targetStatus()
		if err != nil {
			return err
		}
		l.Lock()
		defer l.Unlock()

		for hash, t := range res {
			ret[hash] = t
		}
		return nil
	})
	if err != nil {
		return s.scraping, err
	}
	s.scraping = ret
	return ret, nil
}

// UpdateTarget update the scraping targets of this Group
// every Replicas will compare the new targets to it's targets scraping cache and decide if communicate with sidecar or not,
func (s *Group) UpdateTarget(request *UpdateTargetsRequest) error {
	return s.shardsDo(func(sd *Replicas) error {
		return sd.updateTarget(request)
	})
}

func (s *Group) shardsDo(f func(sd *Replicas) error) error {
	if len(s.replicates) == 0 {
		return fmt.Errorf("no avaliable replicas found in group %s", s.ID)
	}

	g := errgroup.Group{}
	success := false
	for _, tsd := range s.replicates {
		sd := tsd
		g.Go(func() error {
			if err := f(sd); err != nil {
				s.log.Error(err.Error(), sd.ID)
			} else {
				success = true
			}
			return nil
		})
	}
	_ = g.Wait()
	if !success {
		return fmt.Errorf("no success shard in group %s", s.ID)
	}

	return nil
}
