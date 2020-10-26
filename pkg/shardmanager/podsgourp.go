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

package shardmanager

import (
	"fmt"
	"sync"

	v1 "github.com/prometheus/prometheus/web/api/v1"

	"golang.org/x/sync/errgroup"

	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/shard"
)

type pod struct {
	name string
	cli  shard.Client
}

type podsGroup struct {
	id   string
	pods []*pod
	log  logrus.FieldLogger
}

func newPodsGroup(id string, log logrus.FieldLogger) *podsGroup {
	return &podsGroup{
		id:   id,
		pods: []*pod{},
		log:  log,
	}
}

func (s *podsGroup) RuntimeInfo() (*shard.RuntimeInfo, error) {
	ret := &shard.RuntimeInfo{
		Targets: map[string][]*shard.Target{},
	}

	l := sync.Mutex{}
	g := errgroup.Group{}
	success := false
	for _, tsd := range s.pods {
		sd := tsd
		g.Go(func() error {
			r, err := sd.cli.RuntimeInfo()
			if err != nil {
				s.log.Errorf("get runtime info from %s failed : %s", sd.name, err.Error())
				return err
			}
			l.Lock()
			defer l.Unlock()

			if ret.HeadSeries < r.HeadSeries {
				ret = r
			}

			success = true
			return nil
		})
	}
	ret.ID = s.id
	_ = g.Wait()
	if !success {
		return nil, fmt.Errorf("no success shard in group %s", s.id)
	}

	return ret, nil
}

// updateRuntimeInfo tells ShardsGroup about global targets info, include targets other shard tManager
func (s *podsGroup) UpdateRuntimeInfo(r *shard.RuntimeInfo) error {
	g := errgroup.Group{}
	success := false
	r.ID = s.id
	for _, tsd := range s.pods {
		sd := tsd
		g.Go(func() error {
			err := sd.cli.UpdateRuntimeInfo(r)
			if err != nil {
				s.log.Errorf("update runtime info from %s failed : %s", sd.name, err.Error())
				return err
			}
			success = true
			return nil
		})
	}

	_ = g.Wait()
	if !success {
		return fmt.Errorf("no success shard in group %s", s.id)
	}

	return nil
}

func (s *podsGroup) Targets(state string) (*v1.TargetDiscovery, error) {
	g := errgroup.Group{}
	ret := &v1.TargetDiscovery{
		ActiveTargets:  []*v1.Target{},
		DroppedTargets: []*v1.DroppedTarget{},
	}

	success := false
	for _, tsd := range s.pods {
		sd := tsd
		g.Go(func() error {
			ts, err := sd.cli.Targets(state)
			if err != nil {
				s.log.Errorf("update runtime info from %s failed : %s", sd.name, err.Error())
				return err
			}
			ret = ts
			success = true
			return nil
		})
	}

	_ = g.Wait()

	if !success {
		return nil, fmt.Errorf("no success shard in group %s", s.id)
	}

	return ret, nil
}
