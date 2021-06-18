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
package kubernetes

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestReplicasManager_Replicas(t *testing.T) {
	type caseInfo struct {
		stsNamespace     string
		stsSelector      string
		listStatefulSets func(ctx context.Context, opts v12.ListOptions) (*v1.StatefulSetList, error)
		sts              *v1.StatefulSet
		wantShardManager int
		wantErr          bool
	}

	defSts := func() v1.StatefulSet {
		s := &v1.StatefulSet{}
		s.Name = "s"
		s.Namespace = "test"
		s.Status.Replicas = 1
		s.Status.ReadyReplicas = 1
		s.Status.UpdatedReplicas = 1
		return *s
	}

	successCase := func() *caseInfo {
		sts := defSts()
		return &caseInfo{
			stsNamespace: "test",
			stsSelector:  "k8s-app=test",
			sts:          &sts,
			listStatefulSets: func(ctx context.Context, opts v12.ListOptions) (list *v1.StatefulSetList, e error) {
				if opts.LabelSelector != "k8s-app=test" {
					return nil, fmt.Errorf("wrong label selector")
				}
				return &v1.StatefulSetList{
					Items: []v1.StatefulSet{sts},
				}, nil
			},
			wantShardManager: 1,
		}
	}

	var cases = []struct {
		desc       string
		updateCase func(c *caseInfo)
	}{
		{
			desc:       "success",
			updateCase: func(c *caseInfo) {},
		},
		{
			desc: "list shard failed",
			updateCase: func(c *caseInfo) {
				c.listStatefulSets = func(ctx context.Context, opts v12.ListOptions) (list *v1.StatefulSetList, e error) {
					return nil, fmt.Errorf("test")
				}
				c.wantErr = true
			},
		},
		{
			desc: "update replicas != replicas, must skip",
			updateCase: func(c *caseInfo) {
				c.sts.Status.UpdatedReplicas = 0
				c.wantShardManager = 0
			},
		},
		{
			desc: "ready replicas  != replicas",
			updateCase: func(c *caseInfo) {
				c.sts.Status.ReadyReplicas = 0
				c.wantShardManager = 0
			},
		},
	}

	for _, cs := range cases {
		t.Run(cs.desc, func(t *testing.T) {
			r := require.New(t)
			c := successCase()
			cs.updateCase(c)

			m := NewReplicasManager(fake.NewSimpleClientset(), c.stsNamespace, c.stsSelector, 0, true, logrus.New())
			m.listStatefulSets = c.listStatefulSets

			res, err := m.Replicas()
			if c.wantErr {
				r.Error(err)
				return
			}
			r.Equal(c.wantShardManager, len(res))
		})
	}
}
