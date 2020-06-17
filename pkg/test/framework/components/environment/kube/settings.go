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

	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"

	"istio.io/istio/pkg/test/framework/resource"
)

// ClientFactoryFunc is a transformation function that creates the k8s client factories
// from the provided k8s config files.
type AccessorFactoryFunc func(kubeConfigs []string, workDir string) ([]kube.Accessor, error)

// Settings provide kube-specific Settings from flags.
type Settings struct {
	// An array of paths to kube config files. Required if the environment is kubernetes.
	KubeConfig []string

	// AccessorFactoryFunc is an optional override for the default behavior for creating kube.Accessor
	// instances for interacting with clusters. If not specified, Accessors will be created from KubeConfig.
	AccessorFactoryFunc AccessorFactoryFunc

	// Indicates that the Ingress Gateway is not available. This typically happens in Minikube. The Ingress
	// component will fall back to node-port in this case.
	Minikube bool

	// ControlPlaneTopology maps each cluster to the cluster that runs its control plane. For replicated control
	// plane cases (where each cluster has its own control plane), the cluster will map to itself (e.g. 0->0).
	ControlPlaneTopology map[resource.ClusterIndex]resource.ClusterIndex

	// networkTopology is used for the initial assignment of networks to each cluster.
	// The source of truth clusters' networks is the Cluster instances themselves, rather than this field.
	networkTopology map[resource.ClusterIndex]string
}

type SetupSettingsFunc func(s *Settings)

// Setup is a setup function that allows overriding values in the Kube environment settings.
func Setup(sfn SetupSettingsFunc) resource.SetupFn {
	return func(ctx resource.Context) error {
		switch ctx.Environment().EnvironmentName() {
		case environment.Kube:
			sfn(ctx.Environment().(*Environment).s)
		default:
			scopes.Framework.Warnf("kube.SetupSettings: Skipping on non-kube environment: %s", ctx.Environment().EnvironmentName())
		}
		return nil
	}
}

func (s *Settings) clone() *Settings {
	c := *s
	return &c
}

// GetControlPlaneClusters returns a set containing just the cluster indexes that contain control planes.
func (s *Settings) GetControlPlaneClusters() map[resource.ClusterIndex]bool {
	out := make(map[resource.ClusterIndex]bool)
	for _, controlPlaneClusterIndex := range s.ControlPlaneTopology {
		out[controlPlaneClusterIndex] = true
	}
	return out
}

// NewAccessors creates the kubernetes Accessors for interacting with the configured clusters.
func (s *Settings) NewAccessors(workDir string) ([]kube.Accessor, error) {
	newAccessorsFn := s.AccessorFactoryFunc
	if newAccessorsFn == nil {
		newAccessorsFn = newAccessors
	}

	return newAccessorsFn(s.KubeConfig, workDir)
}

// String implements fmt.Stringer
func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("KubeConfig:           %s\n", s.KubeConfig)
	result += fmt.Sprintf("MiniKubeIngress:      %v\n", s.Minikube)
	result += fmt.Sprintf("ControlPlaneTopology: %v\n", s.ControlPlaneTopology)
	result += fmt.Sprintf("NetworkTopology:      %v\n", s.networkTopology)

	return result
}

func newAccessors(kubeConfigs []string, workDir string) ([]kube.Accessor, error) {
	out := make([]kube.Accessor, 0, len(kubeConfigs))
	for _, cfg := range kubeConfigs {
		a, err := kube.NewAccessor(cfg, workDir)
		if err != nil {
			return nil, fmt.Errorf("accessor setup: %v", err)
		}
		out = append(out, a)
	}
	return out, nil
}
