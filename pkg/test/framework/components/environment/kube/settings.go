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
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/scopes"
)

// clusterIndex is the index of a cluster within the kubeconfigsFlag or topology file entries
type clusterIndex int

// clusterTopology defines the associations between multiple clusters in a topology.
type clusterTopology = map[clusterIndex]clusterIndex

// ClientFactoryFunc is a transformation function that creates k8s clients
// from the provided k8s config files.
type ClientFactoryFunc func(kubeConfigs []string) ([]istioKube.ExtendedClient, error)

// Settings provide kube-specific Settings from flags.
type Settings struct {
	// An array of paths to kube config files. Required if the environment is kubernetes.
	kubeconfigsFlag []string

	// Indicates that the Ingress Gateway is not available. This typically happens in minikube. The Ingress
	// component will fall back to node-port in this case.
	minikube bool

	// Indicates that the LoadBalancer services can obtain a public IP. If not, NodePort be used as a workaround
	// for ingress gateway. KinD will not support LoadBalancer out of the box and requires a workaround such as
	// MetalLB.
	LoadBalancerSupported bool

	// controlPlaneTopology maps each cluster to the cluster that runs its control plane. For replicated control
	// plane cases (where each cluster has its own control plane), the cluster will map to itself (e.g. 0->0).
	controlPlaneTopology clusterTopology

	// networkTopology is used for the initial assignment of networks to each cluster.
	// The source of truth clusters' networks is the Cluster instances themselves, rather than this field.
	networkTopology map[clusterIndex]string

	// configTopolgoy maps each cluster to the cluster that runs it's config.
	// If the cluster runs its own config, the cluster will map to itself (e.g. 0->0)
	// By default, we use the controlPlaneTopology as the config topology.
	configTopolgoy clusterTopology
}

func (s *Settings) clone() *Settings {
	c := *s
	return &c
}

func (s *Settings) clusterConfigs() (configs []cluster.Config, err error) {
	if topologyFile == "" {
		// no file, build directly from provided kubeconfigs and topology flag maps
		return s.clusterConfigsFromFlags()
	}

	return s.clusterConfigsFromFile()
}

func (s *Settings) clusterConfigsFromFlags() ([]cluster.Config, error) {
	if len(s.kubeconfigsFlag) == 0 {
		// flag-based, but no kubeconfigs, get kubeconfigs from environment
		scopes.Framework.Info("Flags istio.test.kube.config and istio.test.kube.topology not specified.")
		var err error
		s.kubeconfigsFlag, err = getKubeConfigsFromEnvironment()
		if err != nil {
			return nil, fmt.Errorf("error parsing KubeConfigs from environment: %v", err)
		}

	}
	scopes.Framework.Infof("Using KubeConfigs: %v.", s.kubeconfigsFlag)
	var configs []cluster.Config
	for i, kc := range s.kubeconfigsFlag {
		ci := clusterIndex(i)
		cfg := cluster.Config{
			Name:    fmt.Sprintf("cluster-%d", i),
			Kind:    cluster.Kubernetes,
			Network: s.networkTopology[ci],
			Meta:    map[string]string{"kubeconfig": kc},
		}
		if idx, ok := s.controlPlaneTopology[ci]; ok {
			cfg.PrimaryClusterName = fmt.Sprintf("cluster-%d", idx)
		}
		if idx, ok := s.configTopolgoy[ci]; ok {
			cfg.ConfigClusterName = fmt.Sprintf("cluster-%d", idx)
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

func (s *Settings) clusterConfigsFromFile() ([]cluster.Config, error) {
	scopes.Framework.Infof("Using configs file: %v.", topologyFile)
	filename, err := normalizeFile(topologyFile)
	if err != nil {
		return nil, err
	}
	topologyBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	configs := []cluster.Config{}
	if jerr := json.Unmarshal(topologyBytes, &configs); jerr != nil {
		if yerr := yaml.Unmarshal(topologyBytes, &configs); yerr != nil {
			return nil, fmt.Errorf("failed to parse %s as JSON or YAML", topologyFile)
		}
	}

	// Allow kubeconfig flag to override file
	configs, err = replaceKubeconfigs(configs, s.kubeconfigsFlag)
	if err != nil {
		return nil, err
	}

	// Apply configs overrides from flags, if specified.
	if s.controlPlaneTopology != nil && len(s.controlPlaneTopology) > 0 {
		if len(s.controlPlaneTopology) != len(configs) {
			return nil, fmt.Errorf("istio.test.kube.controlPlaneTopology has %d entries but there are %d clusters", len(controlPlaneTopology), len(configs))
		}
		for src, dst := range s.controlPlaneTopology {
			configs[src].PrimaryClusterName = configs[dst].Name
		}
	}
	if s.configTopolgoy != nil {
		if len(s.configTopolgoy) != len(configs) {
			return nil, fmt.Errorf("istio.test.kube.configTopology has %d entries but there are %d clusters", len(controlPlaneTopology), len(configs))
		}
		for src, dst := range s.controlPlaneTopology {
			configs[src].ConfigClusterName = configs[dst].Name
		}
	}
	if s.networkTopology != nil {
		if len(s.networkTopology) != len(configs) {
			return nil, fmt.Errorf("istio.test.kube.networkTopology has %d entries but there are %d clusters", len(controlPlaneTopology), len(configs))
		}
		for src, network := range s.networkTopology {
			configs[src].ConfigClusterName = network
		}
	}

	return configs, nil
}

// replaceKubeconfigs allows using flags to specify the kubeconfigs for each cluster instead of the topology flags.
// This capability is needed for backwards compatibility and will likely be removed.
func replaceKubeconfigs(configs []cluster.Config, kubeconfigs []string) ([]cluster.Config, error) {
	if len(kubeconfigs) == 0 {
		return configs, nil
	}
	kube := 0
	out := []cluster.Config{}
	for _, config := range configs {
		if config.Kind == cluster.Kubernetes {
			if kube >= len(kubeconfigs) {
				// not enough to cover all clusters in file
				return nil, fmt.Errorf("istio.test.kube.config should have a kubeconfig for each kube cluster")
			}
			if config.Meta == nil {
				config.Meta = map[string]string{}
			}
			config.Meta["kubeconfig"] = kubeconfigs[kube]
		}
		kube++
		out = append(out, config)
	}
	if kube < len(kubeconfigs) {
		return nil, fmt.Errorf("%d kubeconfigs were provided but topolgy has %d kube clusters", len(kubeconfigs), kube)
	}

	return out, nil
}

// String implements fmt.Stringer
func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("Kubeconfigs:           %s\n", s.kubeconfigsFlag)
	result += fmt.Sprintf("LoadBalancerSupported:      %v\n", s.LoadBalancerSupported)
	result += fmt.Sprintf("ControlPlaneTopology: %v\n", s.controlPlaneTopology)
	result += fmt.Sprintf("NetworkTopology:      %v\n", s.networkTopology)
	result += fmt.Sprintf("ConfigTopology:      %v\n", s.configTopolgoy)
	return result
}
