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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
)

const (
	defaultKubeConfig = "~/.kube/config"
)

var (
	// Settings we will collect from the command-line.
	settingsFromCommandLine = &Settings{
		LoadBalancerSupported: true,
	}
	// hold kubeconfigs from command line to split later
	kubeConfigs string
	// hold controlPlaneTopology from command line to parse later
	controlPlaneTopology string
	// hold networkTopology from command line to parse later
	networkTopology string
	// hold configTopology from command line to parse later
	configTopology string
	// file defining all types of topology
	topologyFile string
)

// NewSettingsFromCommandLine returns Settings obtained from command-line flags.
// flag.Parse must be called before calling this function.
func NewSettingsFromCommandLine() (*Settings, error) {
	if !flag.Parsed() {
		panic("flag.Parse must be called before this function")
	}

	s := settingsFromCommandLine.clone()
	if s.minikube {
		return nil, fmt.Errorf("istio.test.kube.minikube is deprecated; set --istio.test.kube.loadbalancer=false instead")
	}

	// Process the kube clusterConfigs.
	var err error
	s.KubeConfig, err = parseKubeConfigs(kubeConfigs, ",")
	if err != nil {
		return nil, fmt.Errorf("error parsing KubeConfigs from command-line: %v", err)
	}

	s.controlPlaneTopology, err = newControlPlaneTopology()
	if err != nil {
		return nil, err
	}

	s.networkTopology, err = parseNetworkTopology()
	if err != nil {
		return nil, err
	}

	s.configTopology, err = newConfigTopology()
	if err != nil {
		return nil, err
	}

	return s, nil
}

func getKubeConfigsFromEnvironment() ([]string, error) {
	// Normalize KUBECONFIG so that it is separated by the OS path list separator.
	// The framework currently supports comma as a separator, but that violates the
	// KUBECONFIG spec.
	value := env.KUBECONFIG.Value()
	if strings.Contains(value, ",") {
		updatedValue := strings.ReplaceAll(value, ",", string(filepath.ListSeparator))
		_ = os.Setenv(env.KUBECONFIG.Name(), updatedValue)
		scopes.Framework.Warnf("KUBECONFIG contains commas: %s.\nReplacing with %s: %s", value,
			filepath.ListSeparator, updatedValue)
		value = updatedValue
	}
	out, err := parseKubeConfigs(value, string(filepath.ListSeparator))
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		scopes.Framework.Info("Environment variable KUBECONFIG unspecified, defaultiing to ~/.kube/config.")
		normalizedDefaultKubeConfig, err := file.NormalizePath(defaultKubeConfig)
		if err != nil {
			return nil, fmt.Errorf("error normalizing default kube config file %s: %v",
				defaultKubeConfig, err)
		}
		out = []string{normalizedDefaultKubeConfig}
	}
	return out, nil
}

func parseKubeConfigs(value, separator string) ([]string, error) {
	if len(value) == 0 {
		return make([]string, 0), nil
	}

	parts := strings.Split(value, separator)
	out := make([]string, 0, len(parts))
	for _, f := range parts {
		f := strings.TrimSpace(f)
		if len(f) != 0 {
			var err error
			if f, err = file.NormalizePath(f); err != nil {
				return nil, err
			}
			out = append(out, f)
		}
	}
	return out, nil
}

func newControlPlaneTopology() (clusterTopology, error) {
	topology, err := parseClusterTopology(controlPlaneTopology)
	if err != nil {
		return nil, err
	}
	if len(topology) == 0 {
		return nil, nil
	}
	return topology, nil
}

func newConfigTopology() (clusterTopology, error) {
	topology, err := parseClusterTopology(configTopology)
	if err != nil {
		return nil, err
	}
	if len(topology) == 0 {
		return nil, nil
	}
	return topology, nil
}

func parseClusterTopology(topology string) (clusterTopology, error) {
	if topology == "" {
		return nil, nil
	}
	out := make(clusterTopology)

	values := strings.Split(configTopology, ",")
	for _, v := range values {
		parts := strings.Split(v, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("failed parsing topology mapping entry %s", v)
		}
		sourceCluster, err := parseClusterIndex(parts[0])
		if err != nil {
			return nil, err
		}
		targetCluster, err := parseClusterIndex(parts[1])
		if err != nil {
			return nil, err
		}
		if _, ok := out[sourceCluster]; ok {
			return nil, fmt.Errorf("multiple mappings for source cluster %d", sourceCluster)
		}
		out[sourceCluster] = targetCluster
	}
	return out, nil
}

func parseNetworkTopology() (map[clusterIndex]string, error) {
	if networkTopology == "" {
		return nil, nil
	}
	out := make(map[clusterIndex]string)
	values := strings.Split(networkTopology, ",")
	for _, v := range values {
		parts := strings.Split(v, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("failed parsing network mapping mapping entry %s", v)
		}
		cluster, err := parseClusterIndex(parts[0])
		if err != nil {
			return nil, err
		}
		if len(parts[1]) == 0 {
			return nil, fmt.Errorf("failed parsing network mapping entry %s: failed parsing network name", v)
		}
		out[cluster] = parts[1]
	}
	return out, nil
}

func parseClusterIndex(index string) (clusterIndex, error) {
	ci, err := strconv.Atoi(index)
	if err != nil || ci < 0 {
		return 0, fmt.Errorf("failed parsing cluster index: %s", index)
	}
	return clusterIndex(ci), nil
}

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.StringVar(&kubeConfigs, "istio.test.kube.config", "",
		"A comma-separated list of paths to kube config files for cluster environments.")
	flag.BoolVar(&settingsFromCommandLine.minikube, "istio.test.kube.minikube", settingsFromCommandLine.minikube,
		"Deprecated. See istio.test.kube.loadbalancer. Setting this flag will fail tests.")
	flag.BoolVar(&settingsFromCommandLine.LoadBalancerSupported, "istio.test.kube.loadbalancer", settingsFromCommandLine.LoadBalancerSupported,
		"Indicates whether or not clusters in the environment support external IPs for LoadBalaner services. Used "+
			"to obtain the right IP address for the Ingress Gateway. Set --istio.test.kube.loadbalancer=false for local KinD/minikube tests."+
			"without MetalLB installed.")
	flag.StringVar(&controlPlaneTopology, "istio.test.kube.controlPlaneTopology",
		"", "Specifies the mapping for each cluster to the cluster hosting its control plane. The value is a "+
			"comma-separated list of the form <clusterIndex>:<controlPlaneClusterIndex>, where the indexes refer to the order in which "+
			"a given cluster appears in the 'istio.test.kube.config' flag. This topology also determines where control planes should "+
			"be deployed. If not specified, the default is to deploy a control plane per cluster (i.e. `replicated control "+
			"planes') and map every cluster to itself (e.g. 0:0,1:1,...).")
	flag.StringVar(&networkTopology, "istio.test.kube.networkTopology",
		"", "Specifies the mapping for each cluster to it's network name, for multi-network scenarios. The value is a "+
			"comma-separated list of the form <clusterIndex>:<networkName>, where the indexes refer to the order in which "+
			"a given cluster appears in the 'istio.test.kube.config' flag. If not specified, network name will be left unset")
	flag.StringVar(&configTopology, "istio.test.kube.configTopology",
		"", "Specifies the mapping for each cluster to the cluster hosting its config. The value is a "+
			"comma-separated list of the form <clusterIndex>:<configClusterIndex>, where the indexes refer to the order in which "+
			"a given cluster appears in the 'istio.test.kube.config' flag. If not specified, the default is every cluster maps to itself(e.g. 0:0,1:1,...).")
	flag.StringVar(&topologyFile, "istio.test.kube.topology", "", "The path to a JSON file that defines control plane,"+
		" network, and config cluster topology. The JSON document should be an array of objects that contain the keys \"control_plane_index\","+
		" \"network_id\" and \"config_index\" with all integer values. If control_plane_index is omitted, the index of the array item is used."+
		"If network_id is omitted, 0 will be used. If config_index is omitted, control_plane_index will be used.")
}
