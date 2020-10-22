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
	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/prom"
)

func initConfig(ctx context.Context, configReload chan *config.Config, lg logrus.FieldLogger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			if err := prom.APIReloadConfig(func() (bytes []byte, e error) {
				return ioutil.ReadFile(cdCfg.ConfigFile)
			}, configReload); err.Err != "" {
				lg.Error(err.Err)
				time.Sleep(time.Second * 3)
			} else {
				return nil
			}
		}
	}
}

func reloadConfig(ctx context.Context, configReload chan *config.Config, lg logrus.FieldLogger, applyConfigs []func(cfg *config.Config) error) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case cfg := <-configReload:
			for _, f := range applyConfigs {
				if err := f(cfg); err != nil {
					lg.Error(err.Error())
				}
			}
		}
	}
}
