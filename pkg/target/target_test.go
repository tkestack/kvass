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
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestTarget_Address(t *testing.T) {
	tar := &Target{
		Labels: labels.Labels{
			{
				Name:  model.AddressLabel,
				Value: "123",
			},
		},
	}
	require.Equal(t, "123", tar.Address())
}

func TestTarget_NoParamURL(t *testing.T) {
	tar := &Target{
		Hash: 0,
		Labels: labels.Labels{
			{
				Name:  model.AddressLabel,
				Value: "127.0.0.1:80",
			},
			{
				Name:  model.SchemeLabel,
				Value: "http",
			},
			{
				Name:  model.MetricsPathLabel,
				Value: "/metrics",
			},
			{
				Name:  model.ParamLabelPrefix + "test",
				Value: "test",
			},
		},
	}
	require.Equal(t, "http://127.0.0.1:80/metrics", tar.NoParamURL().String())
}

func TestTarget_URL(t *testing.T) {
	cfg := &config.ScrapeConfig{
		Params: map[string][]string{
			"t1": {"v1"},
		},
	}

	tar := &Target{
		Hash: 0,
		Labels: labels.Labels{
			{
				Name:  model.AddressLabel,
				Value: "127.0.0.1:80",
			},
			{
				Name:  model.SchemeLabel,
				Value: "http",
			},
			{
				Name:  model.MetricsPathLabel,
				Value: "/metrics",
			},
			{
				Name:  model.ParamLabelPrefix + "t2",
				Value: "v2",
			},
		},
	}
	require.Equal(t, "http://127.0.0.1:80/metrics?t1=v1&t2=v2", tar.URL(cfg).String())
}

func TestTarget_NoReservedLabel(t *testing.T) {
	tar := &Target{
		Hash: 0,
		Labels: labels.Labels{
			{
				Name:  model.AddressLabel,
				Value: "127.0.0.1:80",
			},
			{
				Name:  "instance",
				Value: "a",
			},
		},
	}
	lb := tar.NoReservedLabel()
	require.Equal(t, 1, len(lb))
}
