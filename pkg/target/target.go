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

package target

import (
	"net/url"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
)

const (
	// PrefixForInvalidLabelName is a prefix string for mark invalid label name become valid
	PrefixForInvalidLabelName = model.ReservedLabelPrefix + "invalid_label_"

	// StateNormal indicates this target is scraping normally
	StateNormal = ""
	// StateInTransfer indicate this target is in transfer process
	StateInTransfer = "in_transfer"
)

// Target is a target generate prometheus config
type Target struct {
	// Hash is calculated from origin labels before relabel_configs process and the URL of this target
	// see prometheus scrape.Target.hash
	Hash uint64 `json:"hash"`
	// Labels is result of relabel_configs process
	Labels labels.Labels `json:"labels"`
	// Series is reference series of this target, may from target explorer
	Series int64 `json:"series"`
	// TotalSeries is the total series in last scraping, without metrics_relabel_configs
	TotalSeries int64 `json:"totalSeries"`
	// TargetState indicate current state of this target
	TargetState string `json:"TargetState"`
}

// Address return the address from labels
func (t *Target) Address() string {
	for _, v := range t.Labels {
		if v.Name == model.AddressLabel {
			return v.Value
		}
	}
	return ""
}

// NoReservedLabel return the labels without reserved prefix "__"
func (t *Target) NoReservedLabel() labels.Labels {
	lset := make(labels.Labels, 0, len(t.Labels))
	for _, l := range t.Labels {
		if !strings.HasPrefix(l.Name, model.ReservedLabelPrefix) {
			lset = append(lset, l)
		}
	}
	return lset
}

// NoParamURL return a url without params
func (t *Target) NoParamURL() *url.URL {
	return &url.URL{
		Scheme: t.Labels.Get(model.SchemeLabel),
		Host:   t.Labels.Get(model.AddressLabel),
		Path:   t.Labels.Get(model.MetricsPathLabel),
	}
}

// URL return the full url of this target, the params of cfg will be add to url
func (t *Target) URL(cfg *config.ScrapeConfig) *url.URL {
	params := url.Values{}

	for k, v := range cfg.Params {
		params[k] = make([]string, len(v))
		copy(params[k], v)
	}
	for _, l := range t.Labels {
		if !strings.HasPrefix(l.Name, model.ParamLabelPrefix) {
			continue
		}
		ks := l.Name[len(model.ParamLabelPrefix):]

		if len(params[ks]) > 0 {
			params[ks][0] = l.Value
		} else {
			params[ks] = []string{l.Value}
		}
	}

	return &url.URL{
		Scheme:   t.Labels.Get(model.SchemeLabel),
		Host:     t.Labels.Get(model.AddressLabel),
		Path:     t.Labels.Get(model.MetricsPathLabel),
		RawQuery: params.Encode(),
	}
}
