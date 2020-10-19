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
	"fmt"
	"time"

	"tkestack.io/kvass/pkg/prom"

	"github.com/pkg/errors"

	"golang.org/x/sync/errgroup"

	"tkestack.io/kvass/pkg/shard"

	"github.com/sirupsen/logrus"
)

const (
	MaxExploreTime = 2
)

type Target struct {
	*shard.Target
	LastError          string    `json:"lastError"`
	LastScrape         time.Time `json:"lastScrape"`
	LastScrapeDuration float64   `json:"lastScrapeDuration"`
	Health             string    `json:"health"`

	maxExploreSample int64
	exploreTimes     int
}

func newTarget(t *shard.Target) *Target {
	return &Target{
		Target: t,
		Health: "unknown",
	}
}

func (t *Target) setScrapeErr(duration time.Duration, err error) {
	t.LastScrape = time.Now()
	t.LastScrapeDuration = duration.Seconds()
	if err == nil {
		t.Health = "up"
		t.LastError = ""
	} else {
		t.Health = "down"
		t.LastError = err.Error()
	}
}

// DefTargetManager manager all targets
// DefTargetManager will explore Target if it is unknown
type DefTargetManager struct {
	*prom.ScrapeInfos
	logger  logrus.FieldLogger
	targets map[string]*Target
	total   int64

	needExplore   chan *Target
	maxCon        int
	retryInterval time.Duration
	explore       func(scrapeInfo *prom.ScrapeInfo, url string) (int64, error)
}

func NewDefTargetManager(maxCon int, log logrus.FieldLogger) *DefTargetManager {
	return &DefTargetManager{
		ScrapeInfos:   prom.NewScrapInfos(nil),
		logger:        log,
		needExplore:   make(chan *Target, 10000),
		maxCon:        maxCon,
		explore:       explore,
		retryInterval: time.Second * 5,
		targets:       map[string]*Target{},
	}
}

func (e *DefTargetManager) Update(all []*shard.Target) {
	newTargets := map[string]*Target{}
	newTotal := int64(0)
	for _, tar := range all {
		newTotal++
		hash := tar.Hash()
		old := e.targets[hash]
		if old == nil {
			old = newTarget(tar)
			if old.Samples < 0 {
				e.needExplore <- old
				e.logger.Infof("need explore Target %s", hash)
			}
		}
		newTargets[hash] = old
	}

	e.targets = newTargets
	e.total = newTotal
}

func (e *DefTargetManager) Get(hash string) *Target {
	return e.targets[hash]
}

func (e *DefTargetManager) Run(ctx context.Context) error {
	var g errgroup.Group
	for i := 0; i < e.maxCon; i++ {
		g.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return nil
				case tar := <-e.needExplore:
					if tar == nil {
						continue
					}
					hash := tar.Hash()
					done, err := e.exploreOnce(ctx, tar)
					if !done || err != nil {
						if err != nil {
							e.logger.Errorf("explore failed Target = %s, err = %v", hash, err)
						}
						if !done {
							go func() {
								time.Sleep(e.retryInterval)
								if e.targets[tar.Hash()] != nil {
									e.needExplore <- tar
								}
							}()
						}
					}
				}
			}
		})
	}

	return g.Wait()
}

func (e *DefTargetManager) exploreOnce(ctx context.Context, t *Target) (done bool, err error) {
	start := time.Now()
	defer func() {
		t.setScrapeErr(time.Now().Sub(start), err)
	}()

	info := e.ScrapeInfos.Get(t.JobName)
	if info == nil {
		return false, fmt.Errorf("can not found %s  scrape info", t.JobName)
	}

	samples, err := e.explore(info, t.URL)
	if err != nil {
		return false, errors.Wrapf(err, "explore failed")
	}

	if t.exploreTimes < MaxExploreTime {
		e.logger.Infof("exploring target %s, samples=%d", t.Hash(), samples)
		if t.maxExploreSample < samples {
			t.maxExploreSample = samples
		}
		t.exploreTimes++
		return false, nil
	}

	e.logger.Infof("exploring target %s done, samples=%d", t.Hash())
	t.Samples = t.maxExploreSample
	return true, nil
}

func explore(scrapeInfo *prom.ScrapeInfo, url string) (int64, error) {
	data, typ, err := prom.Scrape(scrapeInfo, url)
	if err != nil {
		return 0, err
	}
	return prom.StatisticSample(data, typ, scrapeInfo.Config.MetricRelabelConfigs)
}
