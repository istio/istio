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
	"strings"
	"sync"

	multierror "github.com/hashicorp/go-multierror"
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
	// The cluster's platform: Kubernetes, Consul, Eureka, CloudFoundry
	ClusterPlatform = "config.istio.io/platform"

	// The cluster's access configuration stored in k8s Secret object
	// E.g., on kubenetes, this file can be usually copied from .kube/config
	ClusterAccessConfigSecret          = "config.istio.io/accessConfigSecret"
	ClusterAccessConfigSecretNamespace = "config.istio.io/accessConfigSecretNamespace"
)

// ClusterStore is a collection of clusters
type ClusterStore struct {
	clusters      []*k8s_cr.Cluster
	clientConfigs map[string]clientcmdapi.Config
	storeLock     sync.RWMutex
}

// NewClustersStore initializes data struct to store clusters information
func NewClustersStore() *ClusterStore {
	return &ClusterStore{
		clusters:      []*k8s_cr.Cluster{},
		clientConfigs: map[string]clientcmdapi.Config{},
	}
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
func (cs *ClusterStore) GetPilotClusters() []*k8s_cr.Cluster {
	return cs.clusters
}

// ReadClusters reads multiple clusters from a ConfigMap
func ReadClusters(k8s kubernetes.Interface, configMapName string,
	configMapNamespace string, cs *ClusterStore) error {

	// getClustersConfigs
	if err := getClustersConfigs(k8s, configMapName, configMapNamespace, cs); err != nil {
		return err
	}
	if len(cs.clusters) == 0 {
		return fmt.Errorf("no kubeconf found in provided ConfigMap: %s/%s", configMapNamespace, configMapName)
	}

	return nil
}

// getClustersConfigs(configMapName,configMapNamespace)
func getClustersConfigs(k8s kubernetes.Interface, configMapName, configMapNamespace string, cs *ClusterStore) error {

	clusterRegistry, err := k8s.CoreV1().ConfigMaps(configMapNamespace).Get(configMapName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for key, data := range clusterRegistry.Data {
		cluster := k8s_cr.Cluster{}
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(data), 4096)
		if err = decoder.Decode(&cluster); err != nil {
			log.Errorf("failed to decode cluster definition for: %s error: %v", key, err)
			continue
		}
		if err := validateCluster(&cluster); err != nil {
			log.Errorf("failed to validate cluster: %s error: %v", key, err)
			continue
		}
		secretName := cluster.ObjectMeta.Annotations[ClusterAccessConfigSecret]
		secretNamespace := cluster.ObjectMeta.Annotations[ClusterAccessConfigSecretNamespace]
		if len(secretName) == 0 {
			log.Errorf("cluster %s does not have annotation for Secret", key)
			continue
		}
		if len(secretNamespace) == 0 {
			secretNamespace = "istio-system"
		}
		kubeconfig, err := getClusterConfigFromSecret(k8s, secretName, secretNamespace, key)
		if err != nil {
			log.Errorf("failed to get Secret %s in namespace %s for cluster %s with error: %v",
				secretName, secretNamespace, key, err)
			continue
		}
		clientConfig, err := clientcmd.Load(kubeconfig)
		if err != nil {
			log.Errorf("failed to load client config from secret %s in namespace %s for cluster %s with error: %v",
				secretName, secretNamespace, key, err)
			continue
		}
		cs.clientConfigs[cluster.ObjectMeta.Name] = *clientConfig
		cs.clusters = append(cs.clusters, &cluster)
	}

	return nil
}

// Read a kubeconfig fragment from the secret.
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
		err = multierror.Append(err, fmt.Errorf("bad kind in configuration: `%s` != 'Cluster'", cluster.TypeMeta.Kind))
	}
	// Default is k8s.
	if len(cluster.ObjectMeta.Annotations[ClusterPlatform]) > 0 {
		switch serviceregistry.ServiceRegistry(cluster.ObjectMeta.Annotations[ClusterPlatform]) {
		// Currently only supporting kubernetes registry,
		case serviceregistry.KubernetesRegistry:
		case serviceregistry.ConsulRegistry:
			fallthrough
		case serviceregistry.EurekaRegistry:
			fallthrough
		case serviceregistry.CloudFoundryRegistry:
			fallthrough
		default:
			err = multierror.Append(err, fmt.Errorf("cluster %s has unsupported platform %s",
				cluster.ObjectMeta.Name, cluster.ObjectMeta.Annotations[ClusterPlatform]))
		}
	}

	if cluster.ObjectMeta.Annotations[ClusterAccessConfigSecret] == "" {
		// by default, expect a secret with the same name as the cluster
		cluster.ObjectMeta.Annotations[ClusterAccessConfigSecretNamespace] = cluster.Name
	}
	if cluster.ObjectMeta.Annotations[ClusterAccessConfigSecretNamespace] == "" {
		cluster.ObjectMeta.Annotations[ClusterAccessConfigSecretNamespace] = "istio-system"
	}

	return
}
