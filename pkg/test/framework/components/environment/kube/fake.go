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

package kube

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/resource"
)

var _ resource.Environment = FakeEnvironment{}

// FakeEnvironment for testing.
type FakeEnvironment struct {
	Name        string
	NumClusters int
	IDValue     string
}

func (f FakeEnvironment) ID() resource.ID {
	return resource.FakeID(f.IDValue)
}

func (f FakeEnvironment) IsMultiCluster() bool {
	return false
}

func (f FakeEnvironment) IsMultiNetwork() bool {
	return false
}

func (f FakeEnvironment) EnvironmentName() string {
	if len(f.Name) == 0 {
		return "fake"
	}
	return f.Name
}

func (f FakeEnvironment) AllClusters() cluster.Clusters {
	res := cluster.Clusters{}
	allClusters := make(cluster.Map)

	for i := 0; i < f.NumClusters; i++ {
		topo := cluster.NewTopology(cluster.Config{
			Name:               fmt.Sprintf("cluster-%d", i),
			PrimaryClusterName: fmt.Sprintf("cluster-%d", i),
			ConfigClusterName:  fmt.Sprintf("cluster-%d", i),
		}, allClusters)
		c := &kube.Cluster{Topology: topo}
		res = append(res, c)
		allClusters[c.Name()] = c
	}
	return res
}

func (f FakeEnvironment) Clusters() cluster.Clusters {
	return f.AllClusters().MeshClusters()
}
