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

package main

import (
	"context"
	"io/ioutil"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/prometheus/config"
	prom_discovery "github.com/prometheus/prometheus/discovery"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"tkestack.io/kvass/pkg/coordinator"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/explore"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/shard"
	k8s_shard "tkestack.io/kvass/pkg/shard/kubernetes"
	"tkestack.io/kvass/pkg/target"
)

var cdCfg = struct {
	shardNamespace string
	shardSelector  string
	shardPort      int
	shardMaxSeries int64
	shadMaxShard   int32
	exploreMaxCon  int
	webAddress     string
	configFile     string
	syncInterval   time.Duration
	configInject   configInjectOption
}{}

func init() {
	coordinatorCmd.Flags().StringVar(&cdCfg.shardNamespace, "shard.namespace", "", "namespace of target shard StatefulSets")
	coordinatorCmd.Flags().StringVar(&cdCfg.shardSelector, "shard.selector", "app.kubernetes.io/name=prometheus", "label selector for select target StatefulSets")
	coordinatorCmd.Flags().IntVar(&cdCfg.shardPort, "shard.port", 8080, "the port of sidecar server")
	coordinatorCmd.Flags().Int64Var(&cdCfg.shardMaxSeries, "shard.max-series", 1000000, "max series of per shard")
	coordinatorCmd.Flags().Int32Var(&cdCfg.shadMaxShard, "shard.max-shard", 999999, "max shard number")
	coordinatorCmd.Flags().IntVar(&cdCfg.exploreMaxCon, "explore.concurrence", 50, "max explore concurrence")
	coordinatorCmd.Flags().StringVar(&cdCfg.webAddress, "web.address", ":9090", "server bind address")
	coordinatorCmd.Flags().StringVar(&cdCfg.configFile, "config.file", "prometheus.yml", "config file path")
	coordinatorCmd.Flags().DurationVar(&cdCfg.syncInterval, "coordinator.interval", time.Second*10, "the interval of coordinator loop")

	coordinatorCmd.Flags().StringVar(&cdCfg.configInject.kubernetes.url, "inject.kubernetes-url", "", "kube-apiserver url to inject to all kubernetes sd")
	coordinatorCmd.Flags().StringVar(&cdCfg.configInject.kubernetes.token, "inject.kubernetes-token", "", "kube-apiserver token to inject to all kubernetes sd")
	coordinatorCmd.Flags().StringVar(&cdCfg.configInject.kubernetes.proxy, "inject.kubernetes-proxy", "", "ckube-apiserver proxy url to inject to all kubernetes sd")

	rootCmd.AddCommand(coordinatorCmd)
}

var coordinatorCmd = &cobra.Command{
	Use:   "coordinator",
	Short: "coordinator manager all prometheus shard",
	Long: `coordinator collects targets information from all shard and 
distribution targets to shards`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cmd.Flags().Parse(args); err != nil {
			return err
		}

		kcfg, err := rest.InClusterConfig()
		if err != nil {
			return err
		}

		cli, err := kubernetes.NewForConfig(kcfg)
		if err != nil {
			return err
		}

		level := &promlog.AllowedLevel{}
		level.Set("info")
		format := &promlog.AllowedFormat{}
		format.Set("logfmt")
		var (
			lg = logrus.New()

			logger = promlog.New(&promlog.Config{
				Level:  level,
				Format: format,
			})

			scrapeManager          = scrape.New()
			discoveryManagerScrape = prom_discovery.NewManager(context.Background(), log.With(logger, "component", "discovery manager scrape"), prom_discovery.Name("scrape"))
			targetDiscovery        = discovery.New(lg.WithField("component", "target discovery"))
			exp                    = explore.New(scrapeManager, lg.WithField("component", "explore"))

			ins = k8s_shard.New(cli, cdCfg.shardNamespace,
				cdCfg.shardSelector,
				cdCfg.shardPort,
				lg.WithField("component", "shard manager"))

			cd = coordinator.NewCoordinator(
				ins,
				cdCfg.shardMaxSeries,
				cdCfg.shadMaxShard,
				cdCfg.syncInterval,
				exp.Get,
				targetDiscovery.ActiveTargets,
				lg.WithField("component", "coordinator"))

			api = coordinator.NewAPI(
				func() (bytes []byte, e error) {
					return ioutil.ReadFile(cdCfg.configFile)
				},
				func(targets map[string][]*discovery.SDTargets) (statuses map[uint64]*target.ScrapeStatus, e error) {
					return getTargetStatus(ins, exp, targets)
				},
				targetDiscovery.ActiveTargets,
				targetDiscovery.DropTargets,
				lg.WithField("component", "web"),
			)
		)

		g := errgroup.Group{}
		ctx := context.Background()

		configReload := make(chan struct{})
		configInit := false

		g.Go(func() error {
			return reloadConfig(ctx, &cdCfg.configInject, api.ConfigReload, lg, []func(cfg *config.Config) error{
				scrapeManager.ApplyConfig,
				exp.ApplyConfig,
				targetDiscovery.ApplyConfig,
				func(cfg *config.Config) error {
					c := make(map[string]prom_discovery.Configs)
					for _, v := range cfg.ScrapeConfigs {
						c[v.JobName] = v.ServiceDiscoveryConfigs
					}
					return discoveryManagerScrape.ApplyConfig(c)
				},
				func(cfg *config.Config) error {
					if !configInit {
						close(configReload)
						configInit = true
					}

					lg.Infof("config reload completed")
					return nil
				},
			})
		})

		g.Go(func() error {
			<-configReload
			lg.Infof("SD start")
			return discoveryManagerScrape.Run()
		})

		g.Go(func() error {
			<-configReload
			lg.Infof("coordinator start")

			return cd.Run(ctx)
		})

		g.Go(func() error {
			<-configReload
			lg.Infof("api start at %s", cdCfg.webAddress)
			return api.Run(cdCfg.webAddress)
		})

		g.Go(func() error {
			<-configReload
			lg.Infof("explore start")
			return exp.Run(ctx, cdCfg.exploreMaxCon)
		})

		g.Go(func() error {
			<-configReload
			lg.Infof("targetDiscovery start")
			return targetDiscovery.Run(ctx, discoveryManagerScrape.SyncCh())
		})

		g.Go(func() error {
			for {
				ts := <-targetDiscovery.ActiveTargetsChan()
				exp.UpdateTargets(ts)
			}
		})

		api.ConfigReload <- initConfig(cdCfg.configFile)
		return g.Wait()
	},
}

func getTargetStatus(manager shard.Manager, exp *explore.Explore, targets map[string][]*discovery.SDTargets) (statuses map[uint64]*target.ScrapeStatus, e error) {
	shards, err := manager.Shards()
	if err != nil {
		return nil, err
	}

	global := map[uint64]*target.ScrapeStatus{}
	l := sync.Mutex{}
	g := errgroup.Group{}
	for _, s := range shards {
		g.Go(func() error {
			res, err := s.TargetStatus()
			if err != nil {
				return err
			}
			l.Lock()
			defer l.Unlock()
			for hash, v := range res {
				global[hash] = v
			}
			return nil
		})
	}

	ret := map[uint64]*target.ScrapeStatus{}
	for job, ts := range targets {
		for _, t := range ts {
			hash := t.ShardTarget.Hash
			rt := global[hash]
			if rt == nil {
				rt = exp.Get(job, hash)
			}
			ret[hash] = rt
		}
	}

	return ret, nil
}
