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
	"encoding/json"
	"io/ioutil"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/sidecar"
	"tkestack.io/kvass/pkg/target"

	"github.com/prometheus/prometheus/config"
	"tkestack.io/kvass/pkg/prom"

	"golang.org/x/sync/errgroup"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var sidecarCfg = struct {
	configFile     string
	configOutFile  string
	proxyAddress   string
	apiAddress     string
	prometheusURL  string
	targetFile     string
	injectProxyURL string
}{}

func init() {
	sidecarCmd.Flags().StringVar(&sidecarCfg.proxyAddress, "web.proxy-addr", ":8008", "proxy listen address")
	sidecarCmd.Flags().StringVar(&sidecarCfg.apiAddress, "web.api-addr", ":8080", "api listen address")
	sidecarCmd.Flags().StringVar(&sidecarCfg.prometheusURL, "prometheus.url", "http://127.0.0.1:9090", "url of target prometheus")
	sidecarCmd.Flags().StringVar(&sidecarCfg.configFile, "config.file", "/etc/prometheus/config_out/prometheus.env.yaml", "origin config file")
	sidecarCmd.Flags().StringVar(&sidecarCfg.configOutFile, "config.output-file", "/etc/prometheus/config_out/prometheus_injected.yaml", "injected config file")
	sidecarCmd.Flags().StringVar(&sidecarCfg.targetFile, "config.target-file", "/prometheus/targets.json", "file to save scraping targets")
	sidecarCmd.Flags().StringVar(&sidecarCfg.injectProxyURL, "inject.proxy", "http://127.0.0.1:8008", "proxy url to inject to all job")
	rootCmd.AddCommand(sidecarCmd)
}

var sidecarCmd = &cobra.Command{
	Use:   "sidecar",
	Short: "sidecar manager one prometheus shard",
	Long:  `sidecar generate a new config file only use static_configs to tell prometheus what to scrape`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := cmd.Flags().Parse(args); err != nil {
			return err
		}
		var (
			lg            = log.New()
			ctx           = context.Background()
			scrapeManager = scrape.New()
			proxy         = sidecar.NewProxy(scrapeManager, log.WithField("component", "target manager"))
			injector      = sidecar.NewInjector(sidecarCfg.configFile, sidecarCfg.configOutFile, sidecar.InjectConfigOptions{
				ProxyURL: sidecarCfg.injectProxyURL,
			}, lg.WithField("component", "injector"))
			promCli = prom.NewClient(sidecarCfg.prometheusURL)
			wb      = sidecar.NewAPI(
				sidecarCfg.prometheusURL,
				func() (bytes []byte, e error) {
					return ioutil.ReadFile(sidecarCfg.configFile)
				},
				promCli.RuntimeInfo,
				proxy.TargetStatus,
				log.WithField("component", "web"),
			)
		)

		configLoaded := make(chan interface{})
		configInit := false
		targetsLoaded := make(chan interface{})
		targetsInit := false

		g := errgroup.Group{}
		g.Go(func() error {
			<-configLoaded
			<-targetsLoaded
			lg.Infof("proxy start at %s", sidecarCfg.proxyAddress)
			return proxy.Run(sidecarCfg.proxyAddress)
		})
		g.Go(func() error {
			<-configLoaded
			<-targetsLoaded
			lg.Infof("web start at %s", sidecarCfg.apiAddress)
			return wb.Run(sidecarCfg.apiAddress)
		})

		g.Go(func() error {
			return reloadConfig(ctx, nil, wb.ConfigReload, lg, []func(cfg *config.Config) error{
				scrapeManager.ApplyConfig,
				func(cfg *config.Config) error {
					return injector.UpdateConfig()
				},
				func(cfg *config.Config) error {
					return promCli.ConfigReload()
				},
				func(cfg *config.Config) error {
					if !configInit {
						close(configLoaded)
						configInit = true
					}
					lg.Infof("config reload completed")
					return nil
				},
			})
		})

		g.Go(func() error {
			return reloadTargets(ctx, wb.TargetReload, lg, []func(map[string][]*target.Target) error{
				proxy.UpdateTargets,
				injector.UpdateTargets,
				func(map[string][]*target.Target) error {
					return promCli.ConfigReload()
				},

				func(targets map[string][]*target.Target) error {
					data, err := json.Marshal(&targets)
					if err != nil {
						return err
					}
					return ioutil.WriteFile(sidecarCfg.targetFile, data, 0755)
				},

				func(targets map[string][]*target.Target) error {
					if !targetsInit {
						close(targetsLoaded)
						targetsInit = true
					}
					lg.Infof("target updated completed")
					return nil
				},
			})
		})

		wb.ConfigReload <- initConfig(sidecarCfg.configFile)
		wb.TargetReload <- initTargets(sidecarCfg.targetFile)
		return g.Wait()
	},
}
