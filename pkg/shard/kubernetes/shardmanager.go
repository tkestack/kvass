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
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/utils/k8sutil"
)

// ShardManager manager shards use kubernetes ShardManager
type ShardManager struct {
	// stsSelector is the label selector of ShardManager
	stsSelector string
	// stsNamespace is the namespace of ShardManager
	stsNamespace string
	// port is the shard client port
	port            int
	cli             kubernetes.Interface
	lg              logrus.FieldLogger
	replicateCache  map[string]*shard.Replicas
	getStatefulSets func() (*v13.StatefulSetList, error)
	getPods         func(lb map[string]string) (*v1.PodList, error)
}

// New create a new StatefulSet shards manager
func New(cli kubernetes.Interface,
	stsNamespace string,
	stsSelector string,
	port int,
	log logrus.FieldLogger) *ShardManager {
	return &ShardManager{
		stsSelector:    stsSelector,
		stsNamespace:   stsNamespace,
		port:           port,
		lg:             log,
		cli:            cli,
		replicateCache: map[string]*shard.Replicas{},
		getStatefulSets: func() (list *v13.StatefulSetList, e error) {
			return cli.AppsV1().StatefulSets(stsNamespace).List(context.TODO(), v12.ListOptions{
				LabelSelector: stsSelector,
			})
		},
		getPods: func(selector map[string]string) (list *v1.PodList, e error) {
			return cli.CoreV1().Pods(stsNamespace).List(context.TODO(), v12.ListOptions{
				LabelSelector: labels.SelectorFromSet(selector).String(),
			})
		},
	}
}

// Shards return current Shards in the cluster
func (s *ShardManager) Shards() ([]*shard.Group, error) {
	stss, err := s.getStatefulSets()
	if err != nil {
		return nil, err
	}

	podss := make([]map[string]v1.Pod, len(stss.Items))
	maxPods := 0
	for i, sts := range stss.Items {
		pods, err := s.getPods(sts.Spec.Template.Labels)
		if err != nil {
			return nil, errors.Wrap(err, "list pod")
		}

		if len(pods.Items) > maxPods {
			maxPods = len(pods.Items)
		}
		podss[i] = map[string]v1.Pod{}
		for _, p := range pods.Items {
			podss[i][p.Name] = p
		}
	}

	ret := make([]*shard.Group, 0)
	for i := 0; i < maxPods; i++ {
		sg := shard.NewGroup(fmt.Sprintf("shard-%d", i), s.lg.WithField("shard", i))
		for index, pods := range podss {
			sts := stss.Items[index]
			p := pods[fmt.Sprintf("%s-%d", sts.Name, i)]
			if !k8sutil.IsPodReady(&p) {
				s.lg.Infof("%s is not ready", p.Name)
				continue
			}

			url := fmt.Sprintf("http://%s:%d", p.Status.PodIP, s.port)
			hash := p.Name + "/" + url
			rp := s.replicateCache[hash]
			if rp == nil {
				rp = shard.NewReplicas(p.Name, url, s.lg.WithField("replicate", p.Name))
				s.replicateCache[hash] = rp
			}

			sg.AddReplicas(rp)
		}
		ret = append(ret, sg)
	}

	return ret, nil
}

// ChangeScale create or delete Shards according to "expReplicate"
func (s *ShardManager) ChangeScale(expect int32) error {
	stss, err := s.cli.AppsV1().StatefulSets(s.stsNamespace).List(context.TODO(), v12.ListOptions{
		LabelSelector: s.stsSelector,
	})
	if err != nil {
		return err
	}

	for _, sts := range stss.Items {
		if *sts.Spec.Replicas >= expect {
			continue
		}

		sts.Spec.Replicas = &expect
		s.lg.Infof("change scale to %d", expect)
		_, err = s.cli.AppsV1().StatefulSets(s.stsNamespace).Update(context.TODO(), &sts, v12.UpdateOptions{})
		if err != nil {
			s.lg.Errorf("update statefuleset %s replicate failed : %s", sts.Name, err.Error())
		}
	}

	return nil
}
