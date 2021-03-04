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
	"path"
	"tkestack.io/kvass/pkg/scrape"
	"tkestack.io/kvass/pkg/sidecar"
	"tkestack.io/kvass/pkg/target"

	"github.com/prometheus/prometheus/config"
	"tkestack.io/kvass/pkg/prom"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var sidecarCfg = struct {
	configFile     string
	configOutFile  string
	proxyAddress   string
	apiAddress     string
	prometheusURL  string
	storePath      string
	injectProxyURL string
	configInject   configInjectOption
}{}

func init() {
	sidecarCmd.Flags().StringVar(&sidecarCfg.proxyAddress, "web.proxy-addr", ":8008", "proxy listen address")
	sidecarCmd.Flags().StringVar(&sidecarCfg.apiAddress, "web.api-addr", ":8080", "api listen address")
	sidecarCmd.Flags().StringVar(&sidecarCfg.prometheusURL, "prometheus.url", "http://127.0.0.1:9090", "url of target prometheus")
	sidecarCmd.Flags().StringVar(&sidecarCfg.configFile, "config.file", "/etc/prometheus/config_out/prometheus.env.yaml", "origin config file")
	sidecarCmd.Flags().StringVar(&sidecarCfg.configOutFile, "config.output-file", "/etc/prometheus/config_out/prometheus_injected.yaml", "injected config file")
	sidecarCmd.Flags().StringVar(&sidecarCfg.storePath, "store.path", "/prometheus/", "path to save shard runtime")
	sidecarCmd.Flags().StringVar(&sidecarCfg.injectProxyURL, "inject.proxy", "http://127.0.0.1:8008", "proxy url to inject to all job")
	sidecarCmd.Flags().StringVar(&sidecarCfg.configInject.kubernetes.serviceAccountPath, "inject.kubernetes-sa-path", "", "change default service account token path")
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
			scrapeManager = scrape.New()
			configManager = prom.NewConfigManager(sidecarCfg.configFile, log.WithField("component", "config manager"))
			targetManager = sidecar.NewTargetsManager(sidecarCfg.storePath, log.WithField("component", "targets manager"))

			proxy = sidecar.NewProxy(
				scrapeManager.GetJob,
				func() map[uint64]*target.ScrapeStatus {
					return targetManager.TargetsInfo().Status
				},
				log.WithField("component", "target manager"))

			injector = sidecar.NewInjector(sidecarCfg.configOutFile, sidecar.InjectConfigOptions{
				ProxyURL:      sidecarCfg.injectProxyURL,
				PrometheusURL: sidecarCfg.prometheusURL,
			}, lg.WithField("component", "injector"))
			promCli = prom.NewClient(sidecarCfg.prometheusURL)
		)

		configManager.AddReloadCallbacks(
			func(cfg *prom.ConfigInfo) error {
				return configInjectSidecar(cfg.Config, &sidecarCfg.configInject)
			},
			scrapeManager.ApplyConfig,
			injector.ApplyConfig,
			func(cfg *prom.ConfigInfo) error {
				return promCli.ConfigReload()
			})

		targetManager.AddUpdateCallbacks(
			injector.UpdateTargets,
			func(map[string][]*target.Target) error {
				return promCli.ConfigReload()
			})

		service := sidecar.NewService(
			sidecarCfg.prometheusURL,
			promCli.RuntimeInfo,
			configManager,
			targetManager,
			log.WithField("component", "web"),
		)

		if err := configManager.Reload(); err != nil {
			panic(err)
		}
		lg.Infof("load config done")

		if err := targetManager.Load(); err != nil {
			panic(err)
		}
		lg.Infof("load targets done")

		g := errgroup.Group{}
		g.Go(func() error {
			lg.Infof("proxy start at %s", sidecarCfg.proxyAddress)
			return proxy.Run(sidecarCfg.proxyAddress)
		})
		g.Go(func() error {
			lg.Infof("sidecar server start at %s", sidecarCfg.apiAddress)
			return service.Run(sidecarCfg.apiAddress)
		})
		return g.Wait()
	},
}

func configInjectSidecar(cfg *config.Config, option *configInjectOption) error {
	if option == nil {
		return nil
	}

	for _, job := range cfg.ScrapeConfigs {
		if option.kubernetes.serviceAccountPath != "" {
			if job.HTTPClientConfig.TLSConfig.CAFile == "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt" {
				job.HTTPClientConfig.TLSConfig.CAFile = path.Join(option.kubernetes.serviceAccountPath, "ca.crt")
			}
			if job.HTTPClientConfig.BearerTokenFile == "/var/run/secrets/kubernetes.io/serviceaccount/token" {
				job.HTTPClientConfig.BearerTokenFile = path.Join(option.kubernetes.serviceAccountPath, "token")
			}
		}
	}
	return nil
}
