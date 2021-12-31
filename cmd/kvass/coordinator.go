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
	"fmt"
	"net/url"
	"path"
	"time"

	"github.com/go-kit/kit/log"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/prometheus/config"
	prom_discovery "github.com/prometheus/prometheus/discovery"
	k8sd "github.com/prometheus/prometheus/discovery/kubernetes"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"tkestack.io/kvass/pkg/prom"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/shard/static"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"tkestack.io/kvass/pkg/coordinator"
	"tkestack.io/kvass/pkg/discovery"
	"tkestack.io/kvass/pkg/explore"
	"tkestack.io/kvass/pkg/scrape"
	k8s_shard "tkestack.io/kvass/pkg/shard/kubernetes"
)

var cdCfg = struct {
	shardType             string
	shardStaticFile       string
	shardNamespace        string
	shardSelector         string
	shardPort             int
	shardMaxSeries        int64
	shardMinShard         int32
	shardMaxShard         int32
	shardMaxIdleTime      time.Duration
	shardDisableAlleviate bool
	shardDeletePVC        bool
	exploreMaxCon         int
	webAddress            string
	configFile            string
	syncInterval          time.Duration
	sdInitTimeout         time.Duration
	configInject          configInjectOption
}{}

func init() {
	coordinatorCmd.Flags().BoolVar(&cdCfg.shardDisableAlleviate, "shard.disable-alleviate", false,
		"disable shard alleviation when shard is overload")
	coordinatorCmd.Flags().StringVar(&cdCfg.shardType, "shard.type", "k8s",
		"type of shard deploy: 'k8s'(default), 'static'")
	coordinatorCmd.Flags().StringVar(&cdCfg.shardStaticFile, "shard.static-file", "static-shards.yaml",
		"static shards config file [shard.type must be 'static']")
	coordinatorCmd.Flags().StringVar(&cdCfg.shardNamespace, "shard.namespace", "",
		"namespace of target shard StatefulSets [shard.type must be 'k8s']")
	coordinatorCmd.Flags().StringVar(&cdCfg.shardSelector, "shard.selector", "app.kubernetes.io/name=prometheus",
		"label selector for select target StatefulSets [shard.type must be 'k8s']")
	coordinatorCmd.Flags().IntVar(&cdCfg.shardPort, "shard.port", 8080,
		"the port of sidecar server")
	coordinatorCmd.Flags().Int64Var(&cdCfg.shardMaxSeries, "shard.max-series", 1000000,
		"max series of per shard")
	coordinatorCmd.Flags().Int32Var(&cdCfg.shardMaxShard, "shard.max-shard", 999999,
		"max shard number")
	coordinatorCmd.Flags().Int32Var(&cdCfg.shardMinShard, "shard.min-shard", 0,
		"min shard number")
	coordinatorCmd.Flags().DurationVar(&cdCfg.shardMaxIdleTime, "shard.max-idle-time", 0,
		"wait time before shard is removed after shard become idle,"+
			"scale down is disabled if this flag is 0")
	coordinatorCmd.Flags().BoolVar(&cdCfg.shardDeletePVC, "shard.delete-pvc", true,
		"kvass will delete pvc when shard is removed")
	coordinatorCmd.Flags().IntVar(&cdCfg.exploreMaxCon, "explore.concurrence", 200,
		"max explore concurrence")
	coordinatorCmd.Flags().StringVar(&cdCfg.webAddress, "web.address", ":9090",
		"server bind address")
	coordinatorCmd.Flags().StringVar(&cdCfg.configFile, "config.file", "prometheus.yml",
		"config file path")
	coordinatorCmd.Flags().DurationVar(&cdCfg.syncInterval, "coordinator.interval", time.Second*10,
		"the interval of coordinator loop")
	coordinatorCmd.Flags().DurationVar(&cdCfg.sdInitTimeout, "sd.init-timeout", time.Minute*1,
		"max time wait for all job first service discovery when coordinator start")
	coordinatorCmd.Flags().StringVar(&cdCfg.configInject.kubernetes.url, "inject.kubernetes-url", "",
		"kube-apiserver url to inject to all kubernetes sd")
	coordinatorCmd.Flags().StringVar(&cdCfg.configInject.kubernetes.proxy, "inject.kubernetes-proxy", "",
		"kube-apiserver proxy url to inject to all kubernetes sd")
	coordinatorCmd.Flags().StringVar(&cdCfg.configInject.kubernetes.serviceAccountPath, "inject.kubernetes-sa-path", "",
		"change default service account token path")
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

			scrapeManager          = scrape.New(lg.WithField("component", "scrape discovery"))
			discoveryManagerScrape = prom_discovery.NewManager(context.Background(), log.With(logger, "component", "discovery manager scrape"), prom_discovery.Name("scrape"))
			targetDiscovery        = discovery.New(lg.WithField("component", "target discovery"))
			exp                    = explore.New(scrapeManager, lg.WithField("component", "explore"))
			cfgManager             = prom.NewConfigManager()

			cd = coordinator.NewCoordinator(
				&coordinator.Option{
					MaxSeries:        cdCfg.shardMaxSeries,
					MaxShard:         cdCfg.shardMaxShard,
					MinShard:         cdCfg.shardMinShard,
					MaxIdleTime:      cdCfg.shardMaxIdleTime,
					Period:           cdCfg.syncInterval,
					DisableAlleviate: cdCfg.shardDisableAlleviate,
				},
				getReplicasManager(lg),
				cfgManager.ConfigInfo,
				exp.Get,
				targetDiscovery.ActiveTargetsByHash,
				lg.WithField("component", "coordinator"))
		)

		cfgManager.AddReloadCallbacks(
			func(cfg *prom.ConfigInfo) error {
				return configInject(cfg.Config, &cdCfg.configInject)
			},
			scrapeManager.ApplyConfig,
			exp.ApplyConfig,
			targetDiscovery.ApplyConfig,
			func(cfg *prom.ConfigInfo) error {
				c := make(map[string]prom_discovery.Configs)
				for _, v := range cfg.Config.ScrapeConfigs {
					c[v.JobName] = v.ServiceDiscoveryConfigs
				}
				return discoveryManagerScrape.ApplyConfig(c)
			},
		)

		svc := coordinator.NewService(
			cdCfg.configFile,
			cfgManager,
			cd.LastGlobalScrapeStatus,
			targetDiscovery.ActiveTargets,
			targetDiscovery.DropTargets,
			lg.WithField("component", "web"),
		)

		if err := cfgManager.ReloadFromFile(cdCfg.configFile); err != nil {
			panic(err)
		}

		g := errgroup.Group{}
		ctx := context.Background()

		g.Go(func() error {
			lg.Infof("SD start")
			return discoveryManagerScrape.Run()
		})

		g.Go(func() error {
			lg.Infof("targetDiscovery start")
			return targetDiscovery.Run(ctx, discoveryManagerScrape.SyncCh())
		})

		g.Go(func() error {
			for {
				ts := <-targetDiscovery.ActiveTargetsChan()
				exp.UpdateTargets(ts)
			}
		})

		g.Go(func() error {
			lg.Infof("explore start")
			return exp.Run(ctx, cdCfg.exploreMaxCon)
		})

		tCtx, cancel := context.WithTimeout(ctx, cdCfg.sdInitTimeout)
		defer cancel()
		if err := targetDiscovery.WaitInit(tCtx); err != nil {
			panic(err)
		}

		g.Go(func() error {
			lg.Infof("coordinator start")
			return cd.Run(ctx)
		})

		g.Go(func() error {
			lg.Infof("api start at %s", cdCfg.webAddress)
			return svc.Run(cdCfg.webAddress)
		})

		return g.Wait()
	},
}

func getReplicasManager(lg logrus.FieldLogger) shard.ReplicasManager {
	switch cdCfg.shardType {
	case "k8s":
		kcfg, err := rest.InClusterConfig()
		if err != nil {
			panic(err)
		}

		cli, err := kubernetes.NewForConfig(kcfg)
		if err != nil {
			panic(err)
		}
		return k8s_shard.NewReplicasManager(cli, cdCfg.shardNamespace,
			cdCfg.shardSelector,
			cdCfg.shardPort,
			cdCfg.shardDeletePVC,
			lg.WithField("component", "shard manager"))

	case "static":
		return static.NewReplicasManager(cdCfg.shardStaticFile, lg.WithField("component", "shard manager"))
	default:
		panic(fmt.Sprintf("unknown shard.type %s", cdCfg.shardType))
	}
}

type configInjectOption struct {
	kubernetes struct {
		url                string
		serviceAccountPath string
		proxy              string
	}
}

func configInject(cfg *config.Config, option *configInjectOption) error {
	if option == nil {
		return nil
	}
	for _, job := range cfg.ScrapeConfigs {
		for _, sd := range job.ServiceDiscoveryConfigs {
			ksd, ok := sd.(*k8sd.SDConfig)
			if ok {
				configInjectK8s(ksd, option)
			}
		}
		configInjectServiceAccount(job, option)
	}
	return nil
}

func configInjectK8s(ksd *k8sd.SDConfig, option *configInjectOption) {
	if ksd.APIServer.URL != nil {
		return
	}

	if option.kubernetes.url != "" {
		u, _ := url.Parse(option.kubernetes.url)
		ksd.APIServer = config_util.URL{URL: u}
	}

	if option.kubernetes.proxy != "" {
		u, _ := url.Parse(option.kubernetes.proxy)
		ksd.HTTPClientConfig.ProxyURL = config_util.URL{URL: u}
	}

	if option.kubernetes.serviceAccountPath != "" {
		if ksd.HTTPClientConfig.TLSConfig.CAFile == "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt" ||
			ksd.HTTPClientConfig.TLSConfig.CAFile == "" {
			ksd.HTTPClientConfig.TLSConfig.CAFile = path.Join(option.kubernetes.serviceAccountPath, "ca.crt")
		}
		if ksd.HTTPClientConfig.BearerTokenFile == "/var/run/secrets/kubernetes.io/serviceaccount/token" ||
			ksd.HTTPClientConfig.BearerTokenFile == "" {
			ksd.HTTPClientConfig.BearerTokenFile = path.Join(option.kubernetes.serviceAccountPath, "token")
		}
	}
}

func configInjectServiceAccount(job *config.ScrapeConfig, option *configInjectOption) {
	if option.kubernetes.serviceAccountPath != "" {
		if job.HTTPClientConfig.TLSConfig.CAFile == "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt" {
			job.HTTPClientConfig.TLSConfig.CAFile = path.Join(option.kubernetes.serviceAccountPath, "ca.crt")
		}

		if job.HTTPClientConfig.BearerTokenFile == "" || job.HTTPClientConfig.BearerTokenFile == "/var/run/secrets/kubernetes.io/serviceaccount/token" {
			job.HTTPClientConfig.BearerTokenFile = path.Join(option.kubernetes.serviceAccountPath, "token")
		}
	}
}
