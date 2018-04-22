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
	"os"
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

// GetClusterID returns a cluster's ID
func GetClusterID(cluster *k8s_cr.Cluster) string {
	if cluster == nil {
		return ""
	}
	return cluster.ObjectMeta.Name
}

// GetPilotClusters return a list of clusters under this pilot, exclude PilotCfgStore
func (cs *ClusterStore) GetPilotClusters() []*k8s_cr.Cluster {
	return cs.clusters
}

// ReadClustersV2 reads multiple clusters based upon the label istio/multiCluster
func ReadClustersV2(k8s kubernetes.Interface, cs *ClusterStore) (errList error) {
	err := getClustersConfigsV2(k8s, cs)
	if err != nil {
		// Errors were encountered, but cluster store was populated
		log.Errorf("The following errors were encountered during multicluster label processing: [ %v ]",
			err)
	}

	return nil
}

// getClustersConfigsV2 reads mutiple clusters from secrets with labels
func getClustersConfigsV2(k8s kubernetes.Interface, cs *ClusterStore) (errList error) {
	podNameSpace := os.Getenv("POD_NAMESPACE")
	clusterSecrets, err := k8s.CoreV1().Secrets(podNameSpace).List(metav1.ListOptions{
		LabelSelector: mcLabel,
	})

	if err != nil {
		return err
	}
	for _, secret := range clusterSecrets.Items {
		cluster := k8s_cr.Cluster{}
		kubeconfig, ok := secret.Data[secret.ObjectMeta.Name]
		if !ok {
			errList = multierror.Append(errList, fmt.Errorf("could not read secret %s error %v", secret.ObjectMeta.Name, err))
			continue
		}
		clientConfig, err := clientcmd.Load(kubeconfig)
		if err != nil {
			errList = multierror.Append(errList, fmt.Errorf("could not load kubeconfig for secret %s error %v", secret.ObjectMeta.Name, err))
			continue
		}

		cs.clientConfigs[secret.ObjectMeta.Name] = *clientConfig
		cluster.ObjectMeta.Name = secret.ObjectMeta.Name
		cs.clusters = append(cs.clusters, &cluster)
	}

	return
}

// ReadClusters reads multiple clusters from a ConfigMap
func ReadClusters(k8s kubernetes.Interface, configMapName string,
	configMapNamespace string, cs *ClusterStore) error {

	// getClustersConfigs populates Cluster Store with valid entries found in
	// the configmap. Partial success is possible when some entries in the configmap
	// are valid and some not.
	err := getClustersConfigs(k8s, configMapName, configMapNamespace, cs)
	if err != nil {
		// Errors were encountered, but cluster store was populated
		log.Errorf("The following errors were encountered during processing of the configmap %s/%s, [ %v ]",
			configMapNamespace, configMapName, err)

	}

	// ALways return nil because Cluster Store has been already initialized and populating it
	// at start up is NOT required, it can be populated at runtime
	return nil
}

// getClustersConfigs(configMapName,configMapNamespace)
func getClustersConfigs(k8s kubernetes.Interface, configMapName, configMapNamespace string, cs *ClusterStore) (errList error) {

	clusterRegistry, err := k8s.CoreV1().ConfigMaps(configMapNamespace).Get(configMapName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for key, data := range clusterRegistry.Data {
		cluster := k8s_cr.Cluster{}
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(data), 4096)
		if err = decoder.Decode(&cluster); err != nil {
			errList = multierror.Append(errList, fmt.Errorf("failed to decode cluster definition for: %s error: %v", key, err))
			continue
		}
		if err := validateCluster(&cluster); err != nil {
			errList = multierror.Append(errList, fmt.Errorf("failed to validate cluster: %s error: %v", key, err))
			continue
		}
		secretName := cluster.ObjectMeta.Annotations[ClusterAccessConfigSecret]
		secretNamespace := cluster.ObjectMeta.Annotations[ClusterAccessConfigSecretNamespace]
		if len(secretName) == 0 {
			errList = multierror.Append(errList, fmt.Errorf("cluster %s does not have annotation for Secret", key))
			continue

		}
		if len(secretNamespace) == 0 {
			secretNamespace = "istio-system"
		}
		kubeconfig, err := getClusterConfigFromSecret(k8s, secretName, secretNamespace, key)
		if err != nil {
			errList = multierror.Append(errList, fmt.Errorf("failed to get Secret %s in namespace %s for cluster %s with error: %v",
				secretName, secretNamespace, key, err))
			continue
		}
		clientConfig, err := clientcmd.Load(kubeconfig)
		if err != nil {
			errList = multierror.Append(errList, fmt.Errorf("failed to load client config from secret %s in namespace %s for cluster %s with error: %v",
				secretName, secretNamespace, key, err))
			continue
		}
		cs.clientConfigs[cluster.ObjectMeta.Name] = *clientConfig
		cs.clusters = append(cs.clusters, &cluster)
	}

	return
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
