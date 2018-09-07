// Copyright 2018 Istio Authors
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

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	k8s_cr "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"

	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
)

// annotations for a Cluster
const (
	// The cluster's platform: Kubernetes, Consul, CloudFoundry
	ClusterPlatform = "config.istio.io/platform"

	// The cluster's access configuration stored in k8s Secret object
	// E.g., on kubernetes, this file can be usually copied from .kube/config
	ClusterAccessConfigSecret          = "config.istio.io/accessConfigSecret"
	ClusterAccessConfigSecretNamespace = "config.istio.io/accessConfigSecretNamespace"

	// Default namespace to fetch cluster's access configuration secret.
	DefaultAccessConfigSecretNamespace = "istio-system"
)

// RemoteCluster defines cluster struct
type RemoteCluster struct {
	Cluster        *k8s_cr.Cluster
	FromSecret     string
	ClusterStatus  string
	Controller     *kube.Controller
	ControlChannel chan struct{}
}

// ClusterStore is a collection of clusters
type ClusterStore struct {
	rc        map[string]*RemoteCluster
	storeLock sync.RWMutex
}

// NewClustersStore initializes data struct to store clusters information
func NewClustersStore() *ClusterStore {
	rc := make(map[string]*RemoteCluster)
	return &ClusterStore{
		rc: rc,
	}
}

// GetClusterID returns a cluster's ID
func GetClusterID(cluster *k8s_cr.Cluster) string {
	if cluster == nil {
		return ""
	}
	return cluster.ObjectMeta.Name
}

// ReadClusters reads multiple clusters from a ConfigMap
func ReadClusters(k8s kubernetes.Interface, configMapName string, configMapNamespace string, cs *ClusterStore) error {

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
			secretNamespace = DefaultAccessConfigSecretNamespace
		}
		kubeconfig, err := getClusterConfigFromSecret(k8s, secretName, secretNamespace, key)
		if err != nil {
			errList = multierror.Append(errList, fmt.Errorf("failed to get Secret %s in namespace %s for cluster %s with error: %v",
				secretName, secretNamespace, key, err))
			continue
		}
		if _, err = clientcmd.Load(kubeconfig); err != nil {
			errList = multierror.Append(errList, fmt.Errorf("failed to load client config from secret %s in namespace %s for cluster %s with error: %v",
				secretName, secretNamespace, key, err))
			continue
		}
		cs.rc[cluster.ObjectMeta.Name] = &RemoteCluster{}
		cs.rc[cluster.ObjectMeta.Name].Cluster = &cluster
	}

	return
}

// Read a kubeconfig fragment from the secret.
func getClusterConfigFromSecret(k8s kubernetes.Interface, secretName string, secretNamespace string, clusterName string) ([]byte, error) {

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
		cluster.ObjectMeta.Annotations[ClusterAccessConfigSecretNamespace] = DefaultAccessConfigSecretNamespace
	}

	return
}
