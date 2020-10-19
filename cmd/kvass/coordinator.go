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
	"time"

	"github.com/prometheus/prometheus/config"

	"golang.org/x/sync/errgroup"

	"tkestack.io/kvass/pkg/shardmanager"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"tkestack.io/kvass/pkg/coordinator"
)

var cdCfg = struct {
	StsNamespace    string
	StsSelector     string
	StsPort         int
	MaxIdleDuration time.Duration
	MaxSeries       int64
	MaxExploreCon   int
	WebAddress      string
	ConfigFile      string
	SyncInterval    time.Duration
}{}

func init() {
	coordinatorCmd.Flags().StringVar(&cdCfg.StsNamespace, "sts-namespace", "", "namespace of target prometheus cluster")
	coordinatorCmd.Flags().StringVar(&cdCfg.StsSelector, "sts-selector", "app.kubernetes.io/name=prometheus", "label selector for select target statefulsets")
	coordinatorCmd.Flags().IntVar(&cdCfg.StsPort, "sts-port", 8080, "the port of kvass shard")
	coordinatorCmd.Flags().DurationVar(&cdCfg.MaxIdleDuration, "max-idle", time.Hour*3, "the wait time before idle instance been deleted")
	coordinatorCmd.Flags().Int64Var(&cdCfg.MaxSeries, "max-series", 1000000, "max series of per instance")
	coordinatorCmd.Flags().IntVar(&cdCfg.MaxExploreCon, "max-concurrence", 50, "max explore concurrence")
	coordinatorCmd.Flags().StringVar(&cdCfg.WebAddress, "listen-address", ":9090", "server bind address")
	coordinatorCmd.Flags().StringVar(&cdCfg.ConfigFile, "config-file", "prometheus.yml", "config file path")
	coordinatorCmd.Flags().DurationVar(&cdCfg.SyncInterval, "sync-interval", time.Second*10, "the interval of coordinator loop")
	rootCmd.AddCommand(coordinatorCmd)
}

var coordinatorCmd = &cobra.Command{
	Use:   "coordinator",
	Short: "coordinator manager all prometheus shard",
	Long: `coordinator collects targets information from all shard and 
calculates the targets list that each shard should be responsible for`,
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

		var (
			lg  = log.New()
			tar = coordinator.NewDefTargetManager(cdCfg.MaxExploreCon, log.WithField("component", "target manager"))
			ins = shardmanager.NewStatefulSet(cli, cdCfg.StsNamespace,
				cdCfg.StsSelector,
				cdCfg.StsPort, log.WithField("component", "shard manager"))
			balancer = coordinator.NewDefBalancer(cdCfg.MaxSeries, tar, log.WithField("component", "balance"))

			cd = coordinator.NewCoordinator(ins,
				balancer.ReBalance,
				cdCfg.SyncInterval,
				log.WithField("component", "coordinator"))

			wb = coordinator.NewWeb(func() (bytes []byte, e error) {
				return ioutil.ReadFile(cdCfg.ConfigFile)
			}, log.WithField("component", "web"))
		)

		g := errgroup.Group{}
		ctx := context.Background()

		g.Go(func() error {
			return cd.Run(ctx)
		})

		g.Go(func() error {
			return wb.Run(cdCfg.WebAddress)
		})

		g.Go(func() error {
			return reloadConfig(ctx, wb.ConfigReload, lg, []func(cfg *config.Config) error{
				tar.ApplyConfig,
			})
		})

		g.Go(func() error {
			return initConfig(ctx, wb.ConfigReload, lg)
		})

		return g.Wait()
	},
}
