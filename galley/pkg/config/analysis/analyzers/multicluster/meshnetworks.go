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

package multicluster

import (
	v1 "k8s.io/api/core/v1"

	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube/secretcontroller"
)

// MeshNetworksAnalyzer validates MeshNetworks configuration in multi-cluster.
type MeshNetworksAnalyzer struct{}

var _ analysis.Analyzer = &MeshNetworksAnalyzer{}

var (
	// Service Registries that are known to istio.
	serviceRegistries = []serviceregistry.ProviderID{
		serviceregistry.Mock,
		serviceregistry.Kubernetes,
		serviceregistry.Consul,
		serviceregistry.MCP,
		serviceregistry.External,
	}
)

// Metadata implements Analyzer
func (s *MeshNetworksAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "meshnetworks.MeshNetworksAnalyzer",
		Description: "Check the validity of MeshNetworks in the cluster",
		Inputs: collection.Names{
			collections.IstioMeshV1Alpha1MeshNetworks.Name(),
			collections.K8SCoreV1Secrets.Name(),
		},
	}
}

// Analyze implements Analyzer
func (s *MeshNetworksAnalyzer) Analyze(c analysis.Context) {
	c.ForEach(collections.K8SCoreV1Secrets.Name(), func(r *resource.Instance) bool {
		if r.Metadata.Labels[secretcontroller.MultiClusterSecretLabel] == "true" {
			s := r.Message.(*v1.Secret)
			for c := range s.Data {
				serviceRegistries = append(serviceRegistries, serviceregistry.ProviderID(c))
			}
		}
		return true
	})

	// only one meshnetworks config should exist.
	c.ForEach(collections.IstioMeshV1Alpha1MeshNetworks.Name(), func(r *resource.Instance) bool {
		mn := r.Message.(*v1alpha1.MeshNetworks)
		for i, n := range mn.Networks {
			for _, e := range n.Endpoints {
				switch re := e.Ne.(type) {
				case *v1alpha1.Network_NetworkEndpoints_FromRegistry:
					found := false
					for _, s := range serviceRegistries {
						if serviceregistry.ProviderID(re.FromRegistry) == s {
							found = true
						}
					}
					if !found {
						c.Report(collections.IstioMeshV1Alpha1MeshNetworks.Name(), msg.NewUnknownMeshNetworksServiceRegistry(r, re.FromRegistry, i))
					}
				}
			}
		}
		return true
	})

}
