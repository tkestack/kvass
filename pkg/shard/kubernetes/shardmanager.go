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
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	v13 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/utils/k8sutil"
)

// shardManager manager shards use kubernetes shardManager
type shardManager struct {
	sts *v13.StatefulSet
	// port is the shard client port
	port      int
	deletePVC bool
	cli       kubernetes.Interface
	lg        logrus.FieldLogger
	getPods   func(lb map[string]string) (*v1.PodList, error)
}

// newShardManager create a new StatefulSet shards manager
func newShardManager(cli kubernetes.Interface,
	sts *v13.StatefulSet,
	port int,
	deletePVC bool,
	log logrus.FieldLogger) *shardManager {
	return &shardManager{
		sts:       sts,
		port:      port,
		lg:        log,
		cli:       cli,
		deletePVC: deletePVC,
		getPods: func(selector map[string]string) (list *v1.PodList, e error) {
			return cli.CoreV1().Pods(sts.Namespace).List(context.TODO(), v12.ListOptions{
				LabelSelector: labels.SelectorFromSet(selector).String(),
			})
		},
	}
}

// Shards return current Shards in the cluster
func (s *shardManager) Shards() ([]*shard.Shard, error) {
	pods, err := s.getPods(s.sts.Spec.Selector.MatchLabels)
	if err != nil {
		return nil, errors.Wrap(err, "list pod")
	}

	sts, err := s.cli.AppsV1().StatefulSets(s.sts.Namespace).Get(context.TODO(), s.sts.Name, v12.GetOptions{})
	if err != nil {
		return nil, err
	}

	podMap := map[string]v1.Pod{}
	for _, p := range pods.Items {
		podMap[p.Name] = p
	}

	ret := make([]*shard.Shard, 0)
	for i := int32(0); i < *sts.Spec.Replicas; i ++ {
		if p, ok := podMap[fmt.Sprintf("%s-%d", sts.Name, i)]; ok {
			url := fmt.Sprintf("http://%s:%d", p.Status.PodIP, s.port)
			ret = append(ret, shard.NewShard(p.Name, url, k8sutil.IsPodReady(&p), s.lg.WithField("shard", p.Name)))
		}
	}

	//for _, p := range pods.Items {
	//	url := fmt.Sprintf("http://%s:%d", p.Status.PodIP, s.port)
	//	ret = append(ret, shard.NewShard(p.Name, url, k8sutil.IsPodReady(&p), s.lg.WithField("shard", p.Name)))
	//}

	return ret, nil
}

// ChangeScale create or delete Shards according to "expReplicate"
func (s *shardManager) ChangeScale(expect int32) error {
	sts, err := s.cli.AppsV1().StatefulSets(s.sts.Namespace).Get(context.TODO(), s.sts.Name, v12.GetOptions{})
	if err != nil {
		return err
	}

	if sts.Spec.Replicas == nil || *sts.Spec.Replicas == expect {
		return nil
	}

	old := *sts.Spec.Replicas
	sts.Spec.Replicas = &expect
	s.lg.Infof("change scale to %d", expect)
	_, err = s.cli.AppsV1().StatefulSets(sts.Namespace).Update(context.TODO(), sts, v12.UpdateOptions{})
	if err != nil {
		return errors.Wrapf(err, "update statefuleset %s replicate failed", sts.Name)

	}

	if s.deletePVC {
		for i := old - 1; i >= expect; i-- {
			for _, pvc := range sts.Spec.VolumeClaimTemplates {
				name := fmt.Sprintf("%s-%s-%d", pvc.Name, sts.Name, i)
				err = s.cli.CoreV1().PersistentVolumeClaims(sts.Namespace).Delete(context.TODO(), name, v12.DeleteOptions{})
				if err != nil && !k8serr.IsNotFound(err) {
					s.lg.Errorf("delete pvc %s failed : %s", name, err.Error())
				}
			}
		}
	}
	return nil
}
