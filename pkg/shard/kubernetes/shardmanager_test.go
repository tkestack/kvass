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
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/sirupsen/logrus"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/require"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
)

func createStatefulSet(t *testing.T, cli kubernetes.Interface, name string, rep int32) {
	r := require.New(t)
	sts1 := &appsv1.StatefulSet{}
	sts1.Name = name
	sts1.Spec.Replicas = &rep
	sts1.Labels = map[string]string{
		"k8s-app": "prometheus",
	}
	sts1.Spec.Selector = &v12.LabelSelector{
		MatchLabels: map[string]string{
			"k8s-app": "prometheus",
			"rep":     name,
		},
	}

	_, err := cli.AppsV1().StatefulSets("default").Create(sts1)
	r.NoError(err)
}

func TestStatefulSet_Shards(t *testing.T) {
	cli := fake.NewSimpleClientset()
	createStatefulSet(t, cli, "rep1", 2)
	createStatefulSet(t, cli, "rep2", 2)

	sts := New(cli, "default", "k8s-app=prometheus", 8080, logrus.New())
	sts.getPods = func(lb map[string]string) (list *v1.PodList, e error) {
		pl := &v1.PodList{}
		for i := 0; i < 2; i++ {
			p := v1.Pod{}
			p.Status.Conditions = []v1.PodCondition{
				{
					Type:   v1.PodReady,
					Status: v1.ConditionTrue,
				},
			}
			pl.Items = append(pl.Items, p)
		}
		return pl, nil
	}

	shards, err := sts.Shards()

	r := require.New(t)
	r.NoError(err)
	r.Equal(2, len(shards))

	for i, s := range shards {
		r.Equal(fmt.Sprintf("shard-%d", i), s.ID)
		r.Equal(2, len(s.Replicas()))
	}
}

func TestStatefulSet_ChangeScale(t *testing.T) {
	r := require.New(t)
	cli := fake.NewSimpleClientset()
	createStatefulSet(t, cli, "rep1", 2)
	sts := New(cli, "default", "k8s-app=prometheus", 8080, logrus.New())
	r.NoError(sts.ChangeScale(10))
	s, err := cli.AppsV1().StatefulSets("default").Get("rep1", v12.GetOptions{})
	r.NoError(err)
	r.Equal(int32(10), *s.Spec.Replicas)
}
