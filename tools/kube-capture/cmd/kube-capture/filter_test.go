// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"testing"

	"gopkg.in/yaml.v2"

	cluster "istio.io/istio/tools/kube-capture/pkg"
)

var (
	testClusterResourcesTree = `
ns1:
  d1:
    p1:
      c1: null
      c2: null
    p2:
      c3: null
  d2:
    p3:
      c4: null
      c5: null
    p4:
      c6: null
`
	testClusterResourcesLabels = `
p1:
  l1: v1
  l2: v2
p2:
  l1: v1
  l2: v22
  l3: v3
p3:
  l1: v1
  l2: v2
  l3: v3
  l4: v4
`
	testClusterResourcesAnnotations = `
p1:
  k2: v2
p2:
  k1: v1
  k2: v2
  k3: v33
p3:
  k1: v1
  k2: v2
  k3: v3
p4:
  k1: v1
  k4: v4
`
	testClusterResources = cluster.Resources{
		Root:        make(map[string]interface{}),
		Labels:      make(map[string]map[string]string),
		Annotations: make(map[string]map[string]string),
	}
)

func init() {
	if err := yaml.Unmarshal([]byte(testClusterResourcesTree), &testClusterResources.Root); err != nil {
		panic(err)
	}
	if err := yaml.Unmarshal([]byte(testClusterResourcesLabels), &testClusterResources.Labels); err != nil {
		panic(err)
	}
	if err := yaml.Unmarshal([]byte(testClusterResourcesAnnotations), &testClusterResources.Annotations); err != nil {
		panic(err)
	}
}

func TestGetMatchingPaths(t *testing.T) {

}
