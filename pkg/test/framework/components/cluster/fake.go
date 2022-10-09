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

package cluster

import (
	"k8s.io/apimachinery/pkg/version"

	"istio.io/istio/pkg/kube"
)

var _ Cluster = FakeCluster{}

func init() {
	RegisterFactory(Fake, newFakeCluster)
}

func NewFake(name, major, minor string) Cluster {
	c, _ := newFakeCluster(
		Config{
			Meta: map[string]any{
				"majorVersion": major,
				"minorVersion": minor,
			},
		},
		Topology{
			ClusterKind: Fake,
			ClusterName: name,
			AllClusters: map[string]Cluster{},
		},
	)
	c.(*FakeCluster).Topology.AllClusters[c.Name()] = c
	return c
}

func newFakeCluster(cfg Config, topology Topology) (Cluster, error) {
	return &FakeCluster{
		CLIClient: kube.MockClient{},
		Topology:  topology,
		Version: &version.Info{
			Major: cfg.Meta.String("majorVesion"),
			Minor: cfg.Meta.String("minorVersion"),
		},
	}, nil
}

// FakeCluster used for testing.
type FakeCluster struct {
	kube.CLIClient
	Version *version.Info
	Topology
}

func (f FakeCluster) GetKubernetesVersion() (*version.Info, error) {
	return f.Version, nil
}
