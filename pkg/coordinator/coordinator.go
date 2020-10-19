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
	"time"

	"github.com/pkg/errors"

	"golang.org/x/sync/errgroup"

	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/shard"
)

// ShardsManager manage all Shard and Shards, it create or delete Shards
type ShardsManager interface {
	// Shards return current Shards in the cluster
	Shards() ([]shard.Client, error)
	// ChangeScale create or delete Shards according to "expReplicate"
	ChangeScale(expReplicate int32) error
}

// Coordinator periodically re balance all shards
type Coordinator struct {
	shardManager ShardsManager
	reBalance    func(rt []*shard.RuntimeInfo) int32
	log          logrus.FieldLogger
	period       time.Duration
}

func NewCoordinator(s ShardsManager,
	reBalance func(rt []*shard.RuntimeInfo) int32,
	period time.Duration,
	log logrus.FieldLogger) *Coordinator {
	return &Coordinator{
		shardManager: s,
		reBalance:    reBalance,
		log:          log,
		period:       period,
	}
}

func (c *Coordinator) Run(ctx context.Context) error {
	tk := time.NewTicker(c.period)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tk.C:
			if err := c.runOnce(); err != nil {
				c.log.Errorf(err.Error())
			}
		}
	}
}

func (c *Coordinator) runOnce() error {
	groups, err := c.shardManager.Shards()
	if err != nil {
		return errors.Wrapf(err, "Shards")
	}

	rt, err := c.globalRuntimeInfo(groups)
	if err != nil {
		return errors.Wrapf(err, "get global runtime info failed")
	}

	replicate := c.reBalance(rt)
	if err := c.applyRuntimeInfo(groups, rt); err != nil {
		return errors.Wrap(err, "sync")
	}

	return c.shardManager.ChangeScale(replicate)
}

func (c *Coordinator) globalRuntimeInfo(gs []shard.Client) ([]*shard.RuntimeInfo, error) {
	var (
		rt  = make([]*shard.RuntimeInfo, len(gs))
		err error
	)

	wait := errgroup.Group{}
	for i, g := range gs {
		index := i
		group := g

		wait.Go(func() error {
			rt[index], err = group.RuntimeInfo()
			if err != nil {
				c.log.Errorf(err.Error())
			}
			return nil
		})
	}

	_ = wait.Wait()
	return rt, nil
}

func (c *Coordinator) applyRuntimeInfo(gs []shard.Client, rt []*shard.RuntimeInfo) error {
	wait := errgroup.Group{}
	for i := range gs {
		index := i
		wait.Go(func() error {
			err := gs[index].UpdateRuntimeInfo(rt[index])
			if err != nil {
				c.log.Errorf(err.Error())
			}
			return nil
		})
	}
	return wait.Wait()
}
