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

package shardmanager

import (
	"fmt"
	"sync"
	"time"

	v13 "k8s.io/api/apps/v1"

	"tkestack.io/kvass/pkg/shard"

	v1 "k8s.io/api/core/v1"

	"github.com/sirupsen/logrus"

	"github.com/pkg/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// StatefulSet manager shards use kubernetes StatefulSet
type StatefulSet struct {
	sync.Mutex
	// stsSelector is the label selector of StatefulSet
	stsSelector string
	// stsNamespace is the namespace of StatefulSet
	stsNamespace string
	// port is the shard client port
	port            int
	cli             kubernetes.Interface
	lg              logrus.FieldLogger
	getStatefulSets func() (*v13.StatefulSetList, error)
	getPods         func(lb map[string]string) (*v1.PodList, error)
}

func NewStatefulSet(cli kubernetes.Interface,
	stsNamespace string,
	stsSelector string,
	port int,
	log logrus.FieldLogger) *StatefulSet {
	return &StatefulSet{
		stsSelector:  stsSelector,
		stsNamespace: stsNamespace,
		port:         port,
		lg:           log,
		cli:          cli,
		getStatefulSets: func() (list *v13.StatefulSetList, e error) {
			return cli.AppsV1().StatefulSets(stsNamespace).List(v12.ListOptions{
				LabelSelector: stsSelector,
			})
		},
		getPods: func(selector map[string]string) (list *v1.PodList, e error) {
			return cli.CoreV1().Pods(stsNamespace).List(v12.ListOptions{
				LabelSelector: labels.SelectorFromSet(selector).String(),
			})
		},
	}
}

func (s *StatefulSet) Shards() ([]shard.Client, error) {
	stss, err := s.getStatefulSets()
	if err != nil {
		return nil, err
	}

	podss := make([][]v1.Pod, len(stss.Items))
	maxPods := 0
	for _, sts := range stss.Items {
		pods, err := s.getPods(sts.Spec.Template.Labels)
		if err != nil {
			return nil, errors.Wrap(err, "list pod")
		}

		if len(pods.Items) > maxPods {
			maxPods = len(pods.Items)
		}

		podss = append(podss, pods.Items)
	}

	ret := make([]shard.Client, 0)
	for i := 0; i < maxPods; i++ {
		sg := newPodsGroup(fmt.Sprintf("shard-%d", i), s.lg.WithField("shard", i))
		for _, pods := range podss {
			if i < len(pods) {
				p := pods[i]
				s := &pod{
					name: p.Name,
					cli:  shard.NewClient(fmt.Sprintf("http://%s:%d", p.Status.PodIP, s.port), time.Second*5),
				}
				sg.pods = append(sg.pods, s)
			}
		}
		ret = append(ret, sg)
	}

	return ret, nil
}

func (s *StatefulSet) ChangeScale(expect int32) error {
	stss, err := s.cli.AppsV1().StatefulSets(s.stsNamespace).List(v12.ListOptions{
		LabelSelector: s.stsSelector,
	})
	if err != nil {
		return err
	}

	for _, sts := range stss.Items {
		if *sts.Spec.Replicas > expect {
			continue
		}

		sts.Spec.Replicas = &expect
		_, err = s.cli.AppsV1().StatefulSets(s.stsNamespace).Update(&sts)
		if err != nil {
			s.lg.Errorf("update statefuleset %s replicate failed : %s", sts.Name, err.Error())
		}
	}

	return nil
}
