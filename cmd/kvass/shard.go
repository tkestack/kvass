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

	"tkestack.io/kvass/pkg/prom"

	"github.com/prometheus/prometheus/config"

	"golang.org/x/sync/errgroup"

	"tkestack.io/kvass/pkg/shard"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var shardCfg = struct {
	// ConfigFile is the path of origin config file
	ConfigFile string
	// ConfigOutFile is the path of injected config file
	ConfigOutFile string
	// ProxyAddress is the address for proxy
	ProxyAddress string
	// ApiAddress is the address of shard api
	ApiAddress string
	// PrometheusUrl is the url of target prometheus
	PrometheusUrl string
	// DataPath is the root path of prometheus data
	DataPath string
}{}

func init() {
	sidecarCmd.Flags().StringVar(&shardCfg.ProxyAddress, "proxy-addr", ":8008", "proxy listen address")
	sidecarCmd.Flags().StringVar(&shardCfg.ApiAddress, "api-addr", ":8080", "api listen address")
	sidecarCmd.Flags().StringVar(&shardCfg.PrometheusUrl, "prometheus-url", "http://127.0.0.1:9090", "url of target prometheus")
	sidecarCmd.Flags().StringVar(&shardCfg.ConfigFile, "config-origin", "/etc/prometheus/config_out/prometheus.env.yaml", "origin config file")
	sidecarCmd.Flags().StringVar(&shardCfg.ConfigOutFile, "config-output", "/etc/prometheus/config_out/prometheus_injected.yaml", "injected config file")
	rootCmd.AddCommand(sidecarCmd)
}

var sidecarCmd = &cobra.Command{
	Use:   "shard",
	Short: "shard manager one prometheus shard",
	Long: `shard hijack all scraping request of prometheus and do local targets manage.
  one target can be scraping normally only if the target is assigned to this shard
  otherwise, en empty result will be send to prometheus`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cmd.Flags().Parse(args); err != nil {
			return err
		}
		var (
			lg      = log.New()
			ctx     = context.Background()
			tm      = shard.NewTargetManager(log.WithField("component", "target manager"))
			proxy   = shard.NewProxy(tm, log.WithField("component", "proxy"))
			runtime = shard.NewRuntimeManager(tm, prom.NewClient(shardCfg.PrometheusUrl), log.WithField("component", "runtime"))
			wb      = shard.NewWeb(
				shardCfg.PrometheusUrl,
				func() (bytes []byte, e error) {
					return ioutil.ReadFile(shardCfg.ConfigFile)
				},
				runtime,
				log.WithField("component", "web"))
		)

		g := errgroup.Group{}
		g.Go(func() error {
			return proxy.Run(shardCfg.ProxyAddress)
		})

		g.Go(func() error {
			return wb.Run(shardCfg.ApiAddress)
		})

		g.Go(func() error {
			return reloadConfig(ctx, wb.ConfigReload, lg, []func(cfg *config.Config) error{
				proxy.ApplyConfig,
				runtime.ApplyConfig,
				func(cfg *config.Config) error {
					_, _, err := shard.InjectConfig(
						func() (bytes []byte, e error) {
							return ioutil.ReadFile(shardCfg.ConfigFile)
						},
						func(bytes []byte) error {
							return ioutil.WriteFile(shardCfg.ConfigOutFile, bytes, 0755)
						},
						shardCfg.PrometheusUrl)
					return err
				},
			})
		})

		return g.Wait()
	},
}
