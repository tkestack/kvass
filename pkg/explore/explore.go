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

package explore

import (
	"context"
	"fmt"
	"github.com/prometheus/prometheus/config"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/utils/types"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"sync"
	"time"
	"tkestack.io/kvass/pkg/target"
)

type exploringTarget struct {
	job    string
	target *target.Target
	rt     *target.ScrapeStatus
}

// Explore will explore Target before it assigned to Shard
type Explore struct {
	logger        logrus.FieldLogger
	scrapeManager *scrape.Manager

	targets     map[string]map[uint64]*exploringTarget
	targetsLock sync.Mutex

	retryInterval time.Duration
	needExplore   chan *exploringTarget
	explore       func(scrapeInfo *scrape.JobInfo, url string) (int64, error)
}

// New create a new Explore
func New(scrapeManager *scrape.Manager, log logrus.FieldLogger) *Explore {
	return &Explore{
		logger:        log,
		scrapeManager: scrapeManager,
		needExplore:   make(chan *exploringTarget, 10000),
		retryInterval: time.Second * 5,
		targets:       map[string]map[uint64]*exploringTarget{},
		explore:       explore,
	}
}

// Get return the target scrape status of the target by hash
func (e *Explore) Get(job string, hash uint64) *target.ScrapeStatus {
	e.targetsLock.Lock()
	defer e.targetsLock.Unlock()

	r := e.targets[job]
	if r == nil || r[hash] == nil {
		return nil
	}

	return r[hash].rt
}

// ApplyConfig delete invalid job's targets according to config
// the new targets will be add by UpdateTargets
func (e *Explore) ApplyConfig(cfg *config.Config) error {
	jobs := make([]string, 0, len(cfg.ScrapeConfigs))
	for _, j := range cfg.ScrapeConfigs {
		jobs = append(jobs, j.JobName)
	}

	e.targetsLock.Lock()
	defer e.targetsLock.Unlock()

	newTargets := map[string]map[uint64]*exploringTarget{}
	for k, v := range e.targets {
		if types.FindString(k, jobs...) {
			newTargets[k] = v
		}
	}
	e.targets = newTargets
	return nil
}

// UpdateTargets update global target info, if target is new , it will send for exploring
func (e *Explore) UpdateTargets(targets map[string][]*discovery.SDTargets) {
	e.targetsLock.Lock()
	defer e.targetsLock.Unlock()

	for job, ts := range targets {
		all := map[uint64]*exploringTarget{}
		for _, t := range ts {
			hash := t.ShardTarget.Hash

			if e.targets[job] != nil && e.targets[job][hash] != nil {
				all[hash] = e.targets[job][hash]
			} else {
				all[hash] = &exploringTarget{
					job:    job,
					rt:     target.NewScrapeStatus(0),
					target: t.ShardTarget,
				}
				e.needExplore <- all[hash]
			}
		}
		e.targets[job] = all
	}
}

// Run start Explore exploring engine
// every Target will be explore MaxExploreTime times
// "con" is the max worker goroutines
func (e *Explore) Run(ctx context.Context, con int) error {
	var g errgroup.Group
	for i := 0; i < con; i++ {
		g.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return nil
				case temp := <-e.needExplore:
					if temp == nil {
						continue
					}
					tar := temp
					hash := tar.target.Hash
					err := e.exploreOnce(ctx, tar)
					if err != nil {
						e.logger.Error(err.Error())
						go func() {
							time.Sleep(e.retryInterval)
							e.targetsLock.Lock()
							defer e.targetsLock.Unlock()
							if e.targets[tar.job] != nil && e.targets[tar.job][hash] != nil {
								e.needExplore <- tar
							}
						}()
					}
				}
			}
		})
	}

	return g.Wait()
}

func (e *Explore) exploreOnce(ctx context.Context, t *exploringTarget) (err error) {
	defer t.rt.SetScrapeErr(time.Now(), err)
	info := e.scrapeManager.GetJob(t.job)
	if info == nil {
		return fmt.Errorf("can not found %s  scrape info", t.job)
	}

	url := t.target.URL(info.Config).String()
	series, err := e.explore(info, url)
	if err != nil {
		return errors.Wrapf(err, "explore failed : %s/%s", t.job, url)
	}

	t.rt.Series = series
	t.target.Series = series
	return nil
}

func explore(scrapeInfo *scrape.JobInfo, url string) (int64, error) {
	data, typ, err := scrapeInfo.Scrape(url)
	if err != nil {
		return 0, err
	}
	return scrape.StatisticSample(data, typ, scrapeInfo.Config.MetricRelabelConfigs)
}
