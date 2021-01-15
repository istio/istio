//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"fmt"
	"istio.io/istio/pkg/test/framework/components/cluster"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

type clusterTopology = map[resource.ClusterIndex]resource.ClusterIndex

// ClientFactoryFunc is a transformation function that creates k8s clients
// from the provided k8s config files.
type ClientFactoryFunc func(kubeConfigs []string) ([]istioKube.ExtendedClient, error)

// Settings provide kube-specific Settings from flags.
type Settings struct {
	// An array of paths to kube config files. Required if the environment is kubernetes.
	KubeConfig []string

	// Indicates that the Ingress Gateway is not available. This typically happens in Minikube. The Ingress
	// component will fall back to node-port in this case.
	Minikube bool

	// Indicates that the LoadBalancer services can obtain a public IP. If not, NodePort be used as a workaround
	// for ingress gateway. KinD will not support LoadBalancer out of the box and requires a workaround such as
	// MetalLB.
	LoadBalancerSupported bool

	// ControlPlaneTopology maps each cluster to the cluster that runs its control plane. For replicated control
	// plane cases (where each cluster has its own control plane), the cluster will map to itself (e.g. 0->0).
	ControlPlaneTopology clusterTopology

	// networkTopology is used for the initial assignment of networks to each cluster.
	// The source of truth clusters' networks is the Cluster instances themselves, rather than this field.
	networkTopology map[resource.ClusterIndex]string

	// ConfigTopology maps each cluster to the cluster that runs it's config.
	// If the cluster runs its own config, the cluster will map to itself (e.g. 0->0)
	// By default, we use the ControlPlaneTopology as the config topology.
	ConfigTopology clusterTopology
}

func (s *Settings) clone() *Settings {
	c := *s
	return &c
}

func (s *Settings) clusterConfigs() []cluster.Config {
	var configs []cluster.Config
	// TODO read entire config from file, use flags for backwards compat
	for i, kc := range s.KubeConfig {
		ci := resource.ClusterIndex(i)
		cfg := cluster.Config{
			Name:    fmt.Sprintf("cluster-%d", i),
			Kind:    cluster.Kubernetes,
			Network: s.networkTopology[ci],
			Meta:    map[string]string{"kubeconfig": kc},
		}
		if idx, ok := s.ControlPlaneTopology[ci]; ok {
			cfg.ControlPlaneClusterName = fmt.Sprintf("cluster-%d", idx)
		}
		if idx, ok := s.ConfigTopology[ci]; ok {
			cfg.ConfigClusterName = fmt.Sprintf("cluster-%d", idx)
		}
		configs = append(configs, cfg)
	}
	return configs
}

// String implements fmt.Stringer
func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("KubeConfig:           %s\n", s.KubeConfig)
	result += fmt.Sprintf("LoadBalancerSupported:      %v\n", s.LoadBalancerSupported)
	result += fmt.Sprintf("ControlPlaneTopology: %v\n", s.ControlPlaneTopology)
	result += fmt.Sprintf("NetworkTopology:      %v\n", s.networkTopology)
	result += fmt.Sprintf("ConfigTopology:      %v\n", s.ConfigTopology)
	return result
}
