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
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestReplicasManager_Replicas(t *testing.T) {
	r := require.New(t)
	rep := NewReplicasManager(fake.NewSimpleClientset(), "", "", 8888, true, logrus.New())
	rep.getStatefulSets = func() (list *v1.StatefulSetList, e error) {
		s := v1.StatefulSet{}
		s.Name = "test"
		list = &v1.StatefulSetList{}
		list.Items = append(list.Items, s)
		return list, nil
	}
	ss, err := rep.Replicas()
	r.NoError(err)
	r.Equal(1, len(ss))
}
