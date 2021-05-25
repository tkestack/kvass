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
	"time"
	"tkestack.io/kvass/pkg/shard"
)

// ReplicasManager select Statefusets to get shard manager
type ReplicasManager struct {
	port            int
	deletePVC       bool
	cli             kubernetes.Interface
	lg              logrus.FieldLogger
	getStatefulSets func() (list *v1.StatefulSetList, e error)
	stsUpdatedTime  map[string]*time.Time
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
		stsUpdatedTime: map[string]*time.Time{},
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
		if s.Status.Replicas != s.Status.UpdatedReplicas {
			g.lg.Warnf("Statefulset %s UpdatedReplicas != Replicas, skipped", s.Name)
			g.stsUpdatedTime[s.Name] = nil
			continue
		}

		if g.stsUpdatedTime[s.Name] == nil {
			t := time.Now()
			g.lg.Warnf("Statefulset %s is not ready, try wait 2m", s.Name)
			g.stsUpdatedTime[s.Name] = &t
		}

		t := g.stsUpdatedTime[s.Name]
		if s.Status.ReadyReplicas != s.Status.Replicas && time.Now().Sub(*t) < time.Minute*2 {
			g.lg.Warnf("Statefulset %s is not ready, still waiting", s.Name)
			continue
		}

		tempS := s
		ret = append(ret, newShardManager(g.cli, &tempS, g.port, g.deletePVC, g.lg.WithField("sts", s.Name)))
	}

	return ret, nil
}
