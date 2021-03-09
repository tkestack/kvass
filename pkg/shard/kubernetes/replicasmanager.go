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
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/apps/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"tkestack.io/kvass/pkg/shard"
)

// ReplicasManager select Statefusets to get shard manager
type ReplicasManager struct {
	port            int
	deletePVC       bool
	cli             kubernetes.Interface
	lg              logrus.FieldLogger
	getStatefulSets func() (list *v1.StatefulSetList, e error)
}

// NewReplicasManager create a ReplicasManager
func NewReplicasManager(
	cli kubernetes.Interface,
	stsNamespace string,
	stsSelector string,
	port int,
	deletePVC bool,
	lg logrus.FieldLogger,
) *ReplicasManager {
	return &ReplicasManager{
		cli:       cli,
		port:      port,
		deletePVC: deletePVC,
		lg:        lg,
		getStatefulSets: func() (list *v1.StatefulSetList, e error) {
			return cli.AppsV1().StatefulSets(stsNamespace).List(context.TODO(), v12.ListOptions{
				LabelSelector: stsSelector,
			})
		},
	}
}

// Replicas return all shards manager
func (g *ReplicasManager) Replicas() ([]shard.Manager, error) {
	sts, err := g.getStatefulSets()
	if err != nil {
		return nil, errors.Wrapf(err, "get statefulset")
	}

	ret := make([]shard.Manager, 0)
	for _, s := range sts.Items {
		tempS := s
		ret = append(ret, newShardManager(g.cli, &tempS, g.port, g.deletePVC, g.lg.WithField("sts", s.Name)))
	}

	return ret, nil
}
