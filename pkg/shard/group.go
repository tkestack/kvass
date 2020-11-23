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
	"time"
	"tkestack.io/kvass/pkg/target"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// RuntimeInfo contains all running status of this shard
type RuntimeInfo struct {
	// HeadSeries return current head_series of prometheus
	HeadSeries int64 `json:"headSeries"`
	// IdleAt is the time this shard become idle
	// it is nil if this shard is not idle
	// TODO: user IdleAt to support shard scaling down
	IdleAt *time.Time `json:"idleAt,omitempty"`
}

// Group is a shard group contains one or more replicates
// it knows how to communicate with shard sidecar and manager local scraping cache
type Group struct {
	ID         string
	log        logrus.FieldLogger
	replicates []*Replicas
}

// NewGroup return a new shard with no replicate
func NewGroup(id string, lg logrus.FieldLogger) *Group {
	return &Group{
		ID:         id,
		log:        lg,
		replicates: []*Replicas{},
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

// TargetsScraping return the targets hash that this Group scraping
// the key of the map is target hash
// the result is union set of all replicates
func (s *Group) TargetsScraping() (map[uint64]bool, error) {
	ret := map[uint64]bool{}
	l := sync.Mutex{}
	err := s.shardsDo(func(sd *Replicas) error {
		res, err := sd.targetsScraping()
		if err != nil {
			return err
		}

		l.Lock()
		defer l.Unlock()

		for k := range res {
			ret[k] = true
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
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
		return nil, err
	}
	return ret, nil
}

// UpdateTarget update the scraping targets of this Group
// every Replicas will compare the new targets to it's targets scraping cache and decide if communicate with sidecar or not,
// request will be send to sidecar only if new activeTargets not eq to the scraping
func (s *Group) UpdateTarget(targets map[string][]*target.Target) error {
	return s.shardsDo(func(sd *Replicas) error {
		return sd.updateTarget(targets)
	})
}

func (s *Group) shardsDo(f func(sd *Replicas) error) error {
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
