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

package shard

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"path"

	"github.com/prometheus/prometheus/config"

	"github.com/pkg/errors"
	config_util "github.com/prometheus/common/config"
	"gopkg.in/yaml.v3"
)

const (
	jobNameFormName = "_jobName"
)

// InjectConfigKubernetesSD contains information to inject to all kubernetes SD jobs
type InjectConfigKubernetesSD struct {
	// APIServerURL is the url of APIServer
	APIServerURL string
	// Token is the bearToken of APIServer
	Token string
	// ProxyURL is the proxy url for access APIServer
	ProxyURL string
}

// InjectConfigOptions indicate what to inject to config file
type InjectConfigOptions struct {
	// ProxyURL will be inject to all job if it is not empty
	ProxyURL string
	// KubernetesSD will be inject to all job that use kubernetesSD
	KubernetesSD InjectConfigKubernetesSD
}

// Injector gen injected config file
type Injector struct {
	// OriginFile is the path to read origin config file
	OriginFile string
	// OutFile is the path to write injected file
	OutFile string
	// TokenDir is the dir to write all BearTokenFile
	// all BearToken config will be translate to BearTokenFile config
	TokenDir  string
	readFile  func(name string) ([]byte, error)
	writeFile func(filename string, data []byte) error
}

// NewInjector create new injector with default file read write
func NewInjector(originFile, outFile, TokenDir string) *Injector {
	return &Injector{
		OriginFile: originFile,
		OutFile:    outFile,
		TokenDir:   TokenDir,
		readFile:   ioutil.ReadFile,
		writeFile: func(filename string, data []byte) error {
			return ioutil.WriteFile(filename, data, 0755)
		},
	}
}

// InjectConfig inject following info to prometheus config
// 1. inject Proxy url for every job: set proxy_url to url of "client sidecar"
// 2. inject jobName for every job: the jobName will be saved into "params" as "_jobName" since
//    sidecar need to known the jobName of every TargetManager request
// 3. change all scheme to http
func (i *Injector) InjectConfig(option InjectConfigOptions) (origin *config.Config, injected *config.Config, err error) {
	cfgData, err := i.readFile(i.OriginFile)
	if err != nil {
		return nil, nil, errors.Wrap(err, "read origin file failed")
	}

	origin = &config.Config{}
	injected = &config.Config{}
	if err := yaml.Unmarshal(cfgData, &origin); err != nil {
		return nil, nil, err
	}

	if err := yaml.Unmarshal(cfgData, &injected); err != nil {
		return nil, nil, err
	}

	for jobID, job := range injected.ScrapeConfigs {
		if job.Params == nil {
			job.Params = map[string][]string{}
		}
		job.Params[jobNameFormName] = []string{job.JobName}

		if option.ProxyURL != "" {
			u, err := url.Parse(option.ProxyURL)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "parse proxy url")
			}
			job.HTTPClientConfig.ProxyURL = config_util.URL{
				URL: u,
			}
			job.Scheme = "http"
		}

		if job.HTTPClientConfig.BearerToken != "" {
			file := path.Join(i.TokenDir, fmt.Sprintf("job_%d_secret", jobID))
			if err := i.writeFile(file, []byte(job.HTTPClientConfig.BearerToken)); err != nil {
				return nil, nil, errors.Wrapf(err, "write token")
			}
			job.HTTPClientConfig.BearerToken = ""
			job.HTTPClientConfig.BearerTokenFile = file
		}

		if option.KubernetesSD.APIServerURL != "" {
			ksd := option.KubernetesSD
			for index, k := range job.ServiceDiscoveryConfig.KubernetesSDConfigs {
				u, err := url.Parse(ksd.APIServerURL)
				if err != nil {
					return nil, nil, errors.Wrapf(err, "parse apiserver url")
				}
				k.APIServer = config_util.URL{URL: u}

				file := path.Join(i.TokenDir, fmt.Sprintf("job_%d_kubernetes_sd_%d_secret", jobID, index))
				if err := i.writeFile(file, []byte(ksd.Token)); err != nil {
					return nil, nil, errors.Wrapf(err, "write token")
				}
				k.HTTPClientConfig.BearerTokenFile = file
				k.HTTPClientConfig.TLSConfig.InsecureSkipVerify = true

				if ksd.ProxyURL != "" {
					u, err := url.Parse(ksd.ProxyURL)
					if err != nil {
						return nil, nil, errors.Wrapf(err, "parse proxy url")
					}
					k.HTTPClientConfig.ProxyURL = config_util.URL{URL: u}
				}
			}
		}
	}

	gen, err := yaml.Marshal(&injected)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "marshal config failed")
	}

	if err := i.writeFile(i.OutFile, gen); err != nil {
		return nil, nil, errors.Wrapf(err, "write file failed")
	}

	return origin, injected, nil
}
