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

package static

import (
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"testing"
)

func TestReplicasManager_Replicas(t *testing.T) {
	type caseInfo struct {
		fileContent  string
		wantReplicas int
		wantErr      bool
	}

	successCase := func() *caseInfo {
		return &caseInfo{
			fileContent: `
replicas:
- shards:
  - id: shard-0
    url: http://1.1.1.1
- shards:
  - id: shard-1
    url: http://2.2.2.2
`,
			wantErr:      false,
			wantReplicas: 2,
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
			desc: "wrong config format",
			updateCase: func(c *caseInfo) {
				c.fileContent = `replicas : a`
				c.wantErr = true
			},
		},
		{
			desc: "read file err",
			updateCase: func(c *caseInfo) {
				c.fileContent = ""
				c.wantErr = true
			},
		},
	}

	for _, cs := range cases {
		t.Run(cs.desc, func(t *testing.T) {
			r := require.New(t)
			c := successCase()
			cs.updateCase(c)
			file := t.TempDir() + "shards.yaml"
			if c.fileContent != "" {
				r.NoError(ioutil.WriteFile(file, []byte(c.fileContent), 0777))
			}

			m := NewReplicasManager(file, logrus.New())
			res, err := m.Replicas()
			if c.wantErr {
				r.Error(err)
				return
			}
			r.Equal(c.wantReplicas, len(res))
		})
	}
}
