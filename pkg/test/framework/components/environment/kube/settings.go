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

	"k8s.io/apimachinery/pkg/runtime/schema"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/config"
	"istio.io/istio/pkg/test/scopes"
)

// clusterIndex is the index of a cluster within the KubeConfig or topology file entries
type clusterIndex int

// clusterTopology defines the associations between multiple clusters in a topology.
type clusterTopology = map[clusterIndex]clusterIndex

// ClientFactoryFunc is a transformation function that creates k8s clients
// from the provided k8s config files.
type ClientFactoryFunc func(kubeConfigs []string) ([]istioKube.CLIClient, error)

// Settings provide kube-specific Settings from flags.
type Settings struct {
	// An array of paths to kube config files. Required if the environment is kubernetes.
	KubeConfig []string

	// Indicates that the LoadBalancer services can obtain a public IP. If not, NodePort be used as a workaround
	// for ingress gateway. KinD will not support LoadBalancer out of the box and requires a workaround such as
	// MetalLB.
	LoadBalancerSupported bool

	// Architecture indicates the architecture of the cluster under test
	Architecture string

	// MCSControllerEnabled indicates that the Kubernetes environment has a Multi-Cluster Services (MCS)
	// controller up and running.
	MCSControllerEnabled bool

	// MCSAPIGroup the group to use for the MCS API
	MCSAPIGroup string

	// MCSAPIVersion the version to use for the MCS API
	MCSAPIVersion string

	// controlPlaneTopology maps each cluster to the cluster that runs its control plane. For replicated control
	// plane cases (where each cluster has its own control plane), the cluster will map to itself (e.g. 0->0).
	controlPlaneTopology clusterTopology

	// networkTopology is used for the initial assignment of networks to each cluster.
	// The source of truth clusters' networks is the Cluster instances themselves, rather than this field.
	networkTopology map[clusterIndex]string

	// configTopology maps each cluster to the cluster that runs it's config.
	// If the cluster runs its own config, the cluster will map to itself (e.g. 0->0)
	// By default, we use the controlPlaneTopology as the config topology.
	configTopology clusterTopology
}

func (s *Settings) clone() *Settings {
	c := *s
	return &c
}

func (s *Settings) clusterConfigs() ([]cluster.Config, error) {
	if len(clusterConfigs) == 0 {
		// not loaded from file file, build directly from provided kubeconfigs and topology flag maps
		return s.clusterConfigsFromFlags()
	}

	return s.clusterConfigsFromFile()
}

func (s *Settings) clusterConfigsFromFlags() ([]cluster.Config, error) {
	if len(s.KubeConfig) == 0 {
		// flag-based, but no kubeconfigs, get kubeconfigs from environment
		scopes.Framework.Info("Flags istio.test.kube.config and istio.test.kube.topology not specified.")
		var err error
		s.KubeConfig, err = getKubeConfigsFromEnvironment()
		if err != nil {
			return nil, fmt.Errorf("error parsing KubeConfigs from environment: %v", err)
		}
	}
	scopes.Framework.Infof("Using KubeConfigs: %v.", s.KubeConfig)
	if err := s.validateTopologyFlags(len(s.KubeConfig)); err != nil {
		return nil, err
	}
	var configs []cluster.Config
	for i, kc := range s.KubeConfig {
		ci := clusterIndex(i)
		cfg := cluster.Config{
			Name:    fmt.Sprintf("cluster-%d", i),
			Kind:    cluster.Kubernetes,
			Network: s.networkTopology[ci],
			Meta:    config.Map{"kubeconfig": kc},
		}
		if idx, ok := s.controlPlaneTopology[ci]; ok {
			cfg.PrimaryClusterName = fmt.Sprintf("cluster-%d", idx)
		}
		if idx, ok := s.configTopology[ci]; ok {
			cfg.ConfigClusterName = fmt.Sprintf("cluster-%d", idx)
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

func (s *Settings) clusterConfigsFromFile() ([]cluster.Config, error) {
	// Allow kubeconfig flag to override file
	var err error
	clusterConfigs, err = replaceKubeconfigs(clusterConfigs, s.KubeConfig)
	if err != nil {
		return nil, err
	}

	if err := s.validateTopologyFlags(len(clusterConfigs)); err != nil {
		return nil, err
	}

	// Apply clusterConfigs overrides from flags, if specified.
	if s.controlPlaneTopology != nil && len(s.controlPlaneTopology) > 0 {
		if len(s.controlPlaneTopology) != len(clusterConfigs) {
			return nil, fmt.Errorf("istio.test.kube.controlPlaneTopology has %d entries but there are %d clusters", len(controlPlaneTopology), len(clusterConfigs))
		}
		for src, dst := range s.controlPlaneTopology {
			clusterConfigs[src].PrimaryClusterName = clusterConfigs[dst].Name
		}
	}
	if s.configTopology != nil {
		if len(s.configTopology) != len(clusterConfigs) {
			return nil, fmt.Errorf("istio.test.kube.configTopology has %d entries but there are %d clusters", len(controlPlaneTopology), len(clusterConfigs))
		}
		for src, dst := range s.controlPlaneTopology {
			clusterConfigs[src].ConfigClusterName = clusterConfigs[dst].Name
		}
	}
	if s.networkTopology != nil {
		if len(s.networkTopology) != len(clusterConfigs) {
			return nil, fmt.Errorf("istio.test.kube.networkTopology has %d entries but there are %d clusters", len(controlPlaneTopology), len(clusterConfigs))
		}
		for src, network := range s.networkTopology {
			clusterConfigs[src].ConfigClusterName = network
		}
	}

	return clusterConfigs, nil
}

func (s *Settings) validateTopologyFlags(nClusters int) error {
	makeErr := func(idx clusterIndex, flag string) error {
		return fmt.Errorf("invalid cluster index %d in %s exceeds %d, the number of configured clusters", idx, flag, nClusters)
	}
	for flag, m := range map[string]clusterTopology{
		"istio.test.kube.controlPlaneTopology": s.controlPlaneTopology,
		"istio.test.kube.configTopology":       s.configTopology,
	} {
		for src, dst := range m {
			if int(src) >= nClusters {
				return makeErr(src, flag)
			}
			if int(dst) >= nClusters {
				return makeErr(dst, flag)
			}
		}
	}
	for idx := range s.networkTopology {
		if int(idx) >= nClusters {
			return makeErr(idx, "istio.test.kube.networkTopology")
		}
	}

	return nil
}

// replaceKubeconfigs allows using flags to specify the kubeconfigs for each cluster instead of the topology flags.
// This capability is needed for backwards compatibility and will likely be removed.
func replaceKubeconfigs(configs []cluster.Config, kubeconfigs []string) ([]cluster.Config, error) {
	if len(kubeconfigs) == 0 {
		return configs, nil
	}
	kube := 0
	out := []cluster.Config{}
	for _, cfg := range configs {
		if cfg.Kind == cluster.Kubernetes {
			if kube >= len(kubeconfigs) {
				// not enough to cover all clusters in file
				return nil, fmt.Errorf("istio.test.kube.cfg should have a kubeconfig for each kube cluster")
			}
			if cfg.Meta == nil {
				cfg.Meta = config.Map{}
			}
			cfg.Meta["kubeconfig"] = kubeconfigs[kube]
		}
		kube++
		out = append(out, cfg)
	}
	if kube < len(kubeconfigs) {
		return nil, fmt.Errorf("%d kubeconfigs were provided but topolgy has %d kube clusters", len(kubeconfigs), kube)
	}

	return out, nil
}

func (s *Settings) MCSAPIGroupVersion() schema.GroupVersion {
	return schema.GroupVersion{
		Group:   s.MCSAPIGroup,
		Version: s.MCSAPIVersion,
	}
}

func (s *Settings) ServiceExportGVR() schema.GroupVersionResource {
	return s.MCSAPIGroupVersion().WithResource("serviceexports")
}

func (s *Settings) ServiceImportGVR() schema.GroupVersionResource {
	return s.MCSAPIGroupVersion().WithResource("serviceimports")
}

// String implements fmt.Stringer
func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("Kubeconfigs:           %s\n", s.KubeConfig)
	result += fmt.Sprintf("LoadBalancerSupported: %v\n", s.LoadBalancerSupported)
	result += fmt.Sprintf("MCSControllerEnabled:  %v\n", s.MCSControllerEnabled)
	result += fmt.Sprintf("ControlPlaneTopology:  %v\n", s.controlPlaneTopology)
	result += fmt.Sprintf("NetworkTopology:       %v\n", s.networkTopology)
	result += fmt.Sprintf("ConfigTopology:        %v\n", s.configTopology)
	return result
}
