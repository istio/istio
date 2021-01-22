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

package fake

import (
	"bytes"
	"fmt"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

var _ resource.Cluster = Cluster{}

func init() {
	cluster.RegisterFactory(factory{})
}

type factory struct {
	configs []cluster.Config
}

func (f factory) Kind() cluster.Kind {
	return cluster.Fake
}

func (f factory) With(configs ...cluster.Config) cluster.Factory {
	return factory{configs: append(f.configs, configs...)}
}

func (f factory) Build(allClusters cluster.Map) (resource.Clusters, error) {
	var clusters resource.Clusters
	for _, cfg := range f.configs {
		clusters = append(clusters, &Cluster{
			Topology: cluster.Topology{
				ClusterName:        cfg.Name,
				Network:            cfg.Network,
				PrimaryClusterName: cfg.PrimaryClusterName,
				ConfigClusterName:  cfg.ConfigClusterName,
				AllClusters:        allClusters,
			},
		})
	}

	return clusters, nil
}

// Cluster used for testing.
type Cluster struct {
	kube.ExtendedClient

	cluster.Topology
}

func (m Cluster) String() string {
	buf := &bytes.Buffer{}

	_, _ = fmt.Fprintf(buf, "Name:               %s\n", m.Name())
	_, _ = fmt.Fprintf(buf, "Kind:               %s\n", cluster.Kubernetes)
	_, _ = fmt.Fprintf(buf, "PrimaryCluster:     %s\n", m.Primary().Name())
	_, _ = fmt.Fprintf(buf, "ConfigCluster:      %s\n", m.Config().Name())
	_, _ = fmt.Fprintf(buf, "Network:            %s\n", m.NetworkName())

	return buf.String()
}
