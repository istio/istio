// Copyright 2017 Istio Authors
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

package clusterregistry

import (
	"fmt"
	"strconv"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	"go.uber.org/multierr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	k8s_cr "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"

	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/log"
)

// annotations for a Cluster
const (
	// the pilot's endpoint IP address where this cluster is part of
	ClusterPilotEndpoint = "config.istio.io/pilotEndpoint"

	// The cluster's platform: Kubernetes, Consul, Eureka, CloudFoundry
	ClusterPlatform = "config.istio.io/platform"

	// The cluster's access configuration stored in k8s Secret object
	// E.g., on kubenetes, this file can be usually copied from .kube/config
	ClusterAccessConfigSecret          = "config.istio.io/accessConfigSecret"
	ClusterAccessConfigSecretNamespace = "config.istio.io/accessConfigSecretNamespace"

	// For the time being, assume that ClusterPilotCfgStore is only set for one cluster only.
	// If set to be true, this cluster will be used as the pilot's config store.
	ClusterPilotCfgStore = "config.istio.io/pilotCfgStore"
)

// ClusterStore is a collection of clusters
type ClusterStore struct {
	clusters      []*k8s_cr.Cluster
	cfgStore      *k8s_cr.Cluster
	clientConfigs map[string]clientcmdapi.Config
}

// GetPilotAccessConfig returns this pilot's access config file name
func (cs *ClusterStore) GetPilotAccessConfig() *clientcmdapi.Config {
	if cs.cfgStore == nil {
		return nil
	}
	pilotAccessConfig := cs.clientConfigs[cs.cfgStore.ObjectMeta.Name]
	return &pilotAccessConfig
}

// GetClientAccessConfigs returns map of collected client configs
func (cs *ClusterStore) GetClientAccessConfigs() map[string]clientcmdapi.Config {
	return cs.clientConfigs
}

// GetClusterAccessConfig returns the access config file of a cluster
func (cs *ClusterStore) GetClusterAccessConfig(cluster *k8s_cr.Cluster) *clientcmdapi.Config {
	if cluster == nil {
		return nil
	}
	clusterAccessConfig := cs.clientConfigs[cluster.ObjectMeta.Name]
	return &clusterAccessConfig
}

// GetClusterName returns a cluster's name
func GetClusterName(cluster *k8s_cr.Cluster) string {
	if cluster == nil {
		return ""
	}
	return cluster.ObjectMeta.Name
}

// GetPilotClusters return a list of clusters under this pilot, exclude PilotCfgStore
func (cs *ClusterStore) GetPilotClusters() (clusters []*k8s_cr.Cluster) {
	if cs.cfgStore != nil {
		pilotEndpoint := cs.cfgStore.ObjectMeta.Annotations[ClusterPilotEndpoint]
		for _, cluster := range cs.clusters {
			if cluster.ObjectMeta.Annotations[ClusterPilotEndpoint] == pilotEndpoint {
				clusters = append(clusters, cluster)
			}
		}
	}
	return
}

func setCfgStore(cs *ClusterStore) error {
	for _, cluster := range cs.clusters {
		log.Infof("ClusterPilotCfgStore: %s", cluster.ObjectMeta.Annotations[ClusterPilotCfgStore])
		if isCfgStore, _ := strconv.ParseBool(cluster.ObjectMeta.Annotations[ClusterPilotCfgStore]); isCfgStore {
			if cs.cfgStore != nil {
				return fmt.Errorf("multiple cluster config stores are defined")
			}
			cs.cfgStore = cluster
		}
	}
	if cs.cfgStore == nil {
		return fmt.Errorf("no config store is defined in the cluster registries")
	}

	return nil
}

// ReadClusters reads multiple clusters from a ConfigMap
func ReadClusters(k8s kubernetes.Interface, configMapName string, configMapNamespace string) (*ClusterStore, error) {

	// getClustersConfigs
	cs, err := getClustersConfigs(k8s, configMapName, configMapNamespace)
	if err != nil {
		return nil, err
	}
	if len(cs.clusters) == 0 {
		return nil, fmt.Errorf("no kubeconf found in provided ConfigMap: %s/%s", configMapNamespace, configMapName)
	}
	if err := setCfgStore(cs); err != nil {
		log.Errorf("%s", err.Error())
		return nil, err
	}

	return cs, nil
}

// getClustersConfigs(configMapName,configMapNamespace)
func getClustersConfigs(k8s kubernetes.Interface, configMapName, configMapNamespace string) (*ClusterStore, error) {
	cs := &ClusterStore{
		clusters:      []*k8s_cr.Cluster{},
		cfgStore:      nil,
		clientConfigs: map[string]clientcmdapi.Config{},
	}

	clusterRegistry, err := k8s.CoreV1().ConfigMaps(configMapNamespace).Get(configMapName, metav1.GetOptions{})
	if err != nil {
		return &ClusterStore{}, err
	}
	cluster := k8s_cr.Cluster{}

	for key, data := range clusterRegistry.Data {
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(data), 4096)
		if err = decoder.Decode(&cluster); err != nil {
			log.Errorf("failed to decode cluster definition for: %s error: %v", key, err)
			return &ClusterStore{}, err
		}
		if err := validateCluster(&cluster); err != nil {
			log.Errorf("failed to validate cluster: %s error: %v", key, err)
			return &ClusterStore{}, err
		}
		secretName := cluster.ObjectMeta.Annotations[ClusterAccessConfigSecret]
		secretNamespace := cluster.ObjectMeta.Annotations[ClusterAccessConfigSecretNamespace]
		if len(secretName) == 0 {
			log.Errorf("cluster %s does not have annotation for Secret", key)
			return &ClusterStore{}, nil
		}
		if len(secretNamespace) == 0 {
			secretNamespace = "istio-system"
		}
		kubeconfig, err := getClusterConfigFromSecret(k8s, secretName, secretNamespace, key)
		if err != nil {
			log.Errorf("failed to get Secret %s in namespace %s for cluster %s with error: %v",
				secretName, secretNamespace, key, err)
			return &ClusterStore{}, nil
		}
		clientConfig, err := clientcmd.Load(kubeconfig)
		if err != nil {
			log.Errorf("failed to load client config from secret %s in namespace %s for cluster %s with error: %v",
				secretName, secretNamespace, key, err)
			return &ClusterStore{}, nil
		}
		cs.clientConfigs[cluster.ObjectMeta.Name] = *clientConfig
	}

	return cs, nil
}

func getClusterConfigFromSecret(k8s kubernetes.Interface,
	secretName string,
	secretNamespace string,
	clusterName string) ([]byte, error) {

	secret, err := k8s.CoreV1().Secrets(secretNamespace).Get(secretName, metav1.GetOptions{})
	if err == nil {
		val, ok := secret.Data[clusterName]
		if !ok {
			log.Errorf("cluster name %s is not found in the secret object: %s/%s",
				clusterName, secret.Name, secret.Namespace)
			return []byte{}, fmt.Errorf("cluster name %s is not found in the secret object: %s/%s",
				clusterName, secret.Name, secret.Namespace)
		}
		return val, nil
	}
	return []byte{}, err
}

// validateCluster validate a cluster
func validateCluster(cluster *k8s_cr.Cluster) (err error) {
	if cluster.TypeMeta.Kind != "Cluster" {
		err = multierr.Append(err, fmt.Errorf("bad kind in configuration: `%s` != 'Cluster'", cluster.TypeMeta.Kind))
	}

	if cluster.ObjectMeta.Annotations[ClusterPilotEndpoint] == "" {
		err = multierror.Append(err, fmt.Errorf("cluster %s doesn't have a valid pilot endpoint", cluster.ObjectMeta.Name))
	}

	switch serviceregistry.ServiceRegistry(cluster.ObjectMeta.Annotations[ClusterPlatform]) {
	case serviceregistry.KubernetesRegistry:
	case serviceregistry.ConsulRegistry:
	case serviceregistry.EurekaRegistry:
	case serviceregistry.CloudFoundryRegistry:
	default:
		err = multierror.Append(err, fmt.Errorf("cluster %s has unsupported platform %s",
			cluster.ObjectMeta.Name, cluster.ObjectMeta.Annotations[ClusterPlatform]))
	}

	if cluster.ObjectMeta.Annotations[ClusterPilotCfgStore] != "" {
		if _, err1 := strconv.ParseBool(cluster.ObjectMeta.Annotations[ClusterPilotCfgStore]); err1 != nil {
			err = multierror.Append(err, err1)
		}
	}
	if cluster.ObjectMeta.Annotations[ClusterAccessConfigSecret] == "" {
		err = multierror.Append(err, fmt.Errorf("cluster %s doesn't have a valid config secret", cluster.ObjectMeta.Name))
	} else {
		if cluster.ObjectMeta.Annotations[ClusterAccessConfigSecretNamespace] == "" {
			cluster.ObjectMeta.Annotations[ClusterAccessConfigSecretNamespace] = "istio-system"
		}
	}

	return
}
