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
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"

	multierror "github.com/hashicorp/go-multierror"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/util/yaml"
	k8s_cr "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"

	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/log"
)

var (
	supportedExtensions = map[string]bool{
		".yaml": true,
		".yml":  true,
	}
)

// annotations for a Cluster
const (
	// the pilot's endpoint IP address where this cluster is part of
	ClusterPilotEndpoint = "config.istio.io/pilotEndpoint"

	// The cluster's platform: Kubernetes, Consul, Eureka, CloudFoundry
	ClusterPlatform = "config.istio.io/platform"

	// The cluster's access configuration file
	// E.g., on kubenetes, this file can be usually copied from .kube/config
	ClusterAccessConfigFile = "config.istio.io/accessConfigFile"

	// For the time being, assume that ClusterPilotCfgStore is only set for one cluster only.
	// If set to be true, this cluster will be used as the pilot's config store.
	ClusterPilotCfgStore = "config.istio.io/pilotCfgStore"
)

// ClusterStore is a collection of clusters
type ClusterStore struct {
	clusters []*k8s_cr.Cluster
	cfgStore *k8s_cr.Cluster
}

// GetPilotAccessConfig returns this pilot's access config file name
func (cs *ClusterStore) GetPilotAccessConfig() string {
	if cs.cfgStore == nil {
		return ""
	}
	return cs.cfgStore.ObjectMeta.Annotations[ClusterAccessConfigFile]
}

// GetClusterAccessConfig returns the access config file of a cluster
func GetClusterAccessConfig(cluster *k8s_cr.Cluster) string {
	if cluster == nil {
		return ""
	}
	return cluster.ObjectMeta.Annotations[ClusterAccessConfigFile]
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

// ReadClusters reads multiple clusters from files in a directory
func ReadClusters(crPath string) (cs *ClusterStore, err error) {
	cs = &ClusterStore{
		clusters: []*k8s_cr.Cluster{},
		cfgStore: nil,
	}

	var clusters []*k8s_cr.Cluster
	err = filepath.Walk(crPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !supportedExtensions[filepath.Ext(path)] || (info.Mode()&os.ModeType) != 0 {
			return nil
		}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			log.Warnf("Failed to read %s: %v", path, err)
			return err
		}
		result, err := parseClusters(crPath, data)
		if err != nil {
			log.Warnf("Failed to parse cluster file %s: %v", path, err)
			return err
		}
		clusters = append(clusters, result...)
		return nil
	})

	if err == nil {
		// For the time being, assume that ClusterPilotCfgStore is only set for one cluster only.
		// This cluster will be used as the pilot's config store.
		for _, cluster := range clusters {
			log.Infof("ClusterPilotCfgStore: %s", cluster.ObjectMeta.Annotations[ClusterPilotCfgStore])
			if isCfgStore, _ := strconv.ParseBool(cluster.ObjectMeta.Annotations[ClusterPilotCfgStore]); isCfgStore {
				if cs.cfgStore != nil {
					err = fmt.Errorf("multiple cluster config stores are defined")
					log.Warnf("%v", err)
					return nil, err
				}
				cs.cfgStore = cluster
			} else {
				cs.clusters = append(cs.clusters, cluster)
			}
		}
		if cs.cfgStore == nil {
			log.Warnf("no config store is defined in the cluster registries")
			return nil, nil
		}
	}

	return
}

// validateCluster validate a cluster
func validateCluster(crPath string, cluster *k8s_cr.Cluster) (err error) {
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

	if cluster.ObjectMeta.Annotations[ClusterAccessConfigFile] == "" {
		err = multierror.Append(err, fmt.Errorf("cluster %s doesn't have a valid config file", cluster.ObjectMeta.Name))
	} else {
		cfgFile := path.Join(crPath, cluster.ObjectMeta.Annotations[ClusterAccessConfigFile])
		if _, err1 := os.Stat(cfgFile); err1 != nil {
			err = multierror.Append(err, err1)
		}
	}
	return
}

// parseClusters reads multiple clusters from a single file
func parseClusters(crPath string, clusterData []byte) (clusters []*k8s_cr.Cluster, err error) {
	reader := bytes.NewReader(clusterData)
	var empty = k8s_cr.Cluster{}

	// We store configs as a YaML stream; there may be more than one decoder.
	yamlDecoder := yaml.NewYAMLOrJSONDecoder(reader, 512*1024)
	for {
		obj := k8s_cr.Cluster{}
		err1 := yamlDecoder.Decode(&obj)
		if err1 == io.EOF {
			break
		}
		if err1 != nil {
			err = multierror.Append(err, err1)
			return nil, err
		}
		if reflect.DeepEqual(obj, empty) {
			continue
		}

		err1 = validateCluster(crPath, &obj)
		if err1 == nil {
			clusters = append(clusters, &obj)
		} else {
			err = multierror.Append(err, err1)
		}
	}

	return
}
