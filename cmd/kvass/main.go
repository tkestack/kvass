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
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	_ "net/http/pprof"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "Kvass",
	Short: `Prometheus sharding`,
	Long: `Kvass is a noninvasive PrometheusURL scaling solution,
it allows PrometheusURL shard with "target" granules.`,
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	null, _ := os.Open(os.DevNull)
	gin.DefaultWriter = null
	rand.Seed(time.Now().UnixNano())
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
