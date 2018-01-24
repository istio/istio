/*
Copyright 2018 Istio Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package clusterregistry

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/util/yaml"
	"fmt"
	"strconv"
)

var (
	supportedExtensions = map[string]bool{
		".yaml": true,
		".yml":  true,
	}
)

// annotations for a Cluster
const (
	ClusterPilotEndpoint    = "config.istio.io/pilotEndpoint"
	ClusterPlatform         = "config.istio.io/platform"
	ClusterAccessConfigFile = "config.istio.io/accessConfigFile"
	ClusterPilotCfgStore    = "config.istio.io/pilotCfgStore"
)

// ClusterStore is a collection of clusters
type ClusterStore struct {
	clusters []*Cluster
	cfgStore *Cluster
}

// GetPilotAccessConfig returns this pilot's access config file name
func (cs *ClusterStore) GetPilotAccessConfig() string {
	for _, cluster := range cs.clusters {
		if cluster.ObjectMeta.Annotations[ClusterPilotCfgStore] != "" {
			if b, _ := strconv.ParseBool(cluster.ObjectMeta.Annotations[ClusterPilotCfgStore]); b {
				cs.cfgStore = cluster
				glog.V(2).Infof("ConfigStore: %s", cluster.ObjectMeta.Annotations[ClusterAccessConfigFile])
				return cluster.ObjectMeta.Annotations[ClusterAccessConfigFile]
			}
		}
	}
	return ""
}

// GetClusterAccessConfig returns the access config file of a cluster
func GetClusterAccessConfig(cluster *Cluster) string {
	return cluster.ObjectMeta.Annotations[ClusterAccessConfigFile]
}

// GetClusterName returns a cluster's name
func GetClusterName(cluster *Cluster) string {
	return cluster.ObjectMeta.Name
}

// GetPilotClusters return a list of clusters under this pilot, exclude PilotCfgStore
func (cs *ClusterStore) GetPilotClusters() (clusters []*Cluster) {
	if cs.cfgStore != nil {
		pilotEndpoint := cs.cfgStore.ObjectMeta.Annotations[ClusterPilotEndpoint]
		for _, cluster := range cs.clusters {
			isPilotCfgStore := false
			if cluster.ObjectMeta.Annotations[ClusterPilotCfgStore] != "" {
				isPilotCfgStore, _ = strconv.ParseBool(cluster.ObjectMeta.Annotations[ClusterPilotCfgStore])
			}
			if !isPilotCfgStore && cluster.ObjectMeta.Annotations[ClusterPilotEndpoint] == pilotEndpoint {
				glog.V(2).Infof("accessConfig: %s", cluster.ObjectMeta.Annotations[ClusterAccessConfigFile])
				clusters = append(clusters, cluster)
			}
		}
	}
	return
}

// ReadClusters reads multiple clusters from files in a directory
func ReadClusters(crPath string) (cs *ClusterStore, err error) {
	cs = &ClusterStore{
		clusters: []*Cluster{},
		cfgStore: nil,
	}
	err = filepath.Walk(crPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !supportedExtensions[filepath.Ext(path)] || (info.Mode()&os.ModeType) != 0 {
			return nil
		}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			glog.Warningf("Failed to read %s: %v", path, err)
			return err
		}
		result, err := parseClusters(data)
		if err != nil {
			glog.Warningf("Failed to parse cluster file %s: %v", path, err)
			return err
		}
		cs.clusters = append(cs.clusters, result...)
		return nil
	})

	return
}

//
func parseClusters(inputs []byte) (clusters []*Cluster, err error) {
	reader := bytes.NewReader([]byte(inputs))
	var empty = Cluster{}

	// We store configs as a YaML stream; there may be more than one decoder.
	yamlDecoder := yaml.NewYAMLOrJSONDecoder(reader, 512*1024)
	for {
		obj := Cluster{}
		err = yamlDecoder.Decode(&obj)
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			return nil, err
		}
		if reflect.DeepEqual(obj, empty) {
			continue
		}

		if obj.TypeMeta.Kind != "Cluster" {
			return clusters, fmt.Errorf("Bad kind in configuration: `%s` != 'Cluster'",
				obj.TypeMeta.Kind)
		}
		clusters = append(clusters, &obj)
	}

	return
}
