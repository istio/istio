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

package framework

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"

	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/util/yaml"
	k8s_cr "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"

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

	// For the time being, assume that ClusterPilotCfgStore is only set for one cluster.
	// If set to be true, this cluster will be used as the pilot's config store.
	ClusterPilotCfgStore = "config.istio.io/pilotCfgStore"
)

func getKubeConfigFromFile(dirname string) (string, string, error) {

	var clusters []*k8s_cr.Cluster
	err := filepath.Walk(dirname, func(path string, info os.FileInfo, err error) error {
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
		result, err := parseClusters(dirname, data)
		if err != nil {
			log.Warnf("Failed to parse cluster file %s: %v", path, err)
			return err
		}
		clusters = append(clusters, result...)
		return nil
	})

	var localKube, remoteKube string
	if err == nil {
		// The tests assume that ClusterPilotCfgStore is only set for one cluster only.
		// This cluster will be used as the pilot's config store.
		for _, cluster := range clusters {
			log.Infof("ClusterPilotCfgStore: %s", cluster.ObjectMeta.Annotations[ClusterPilotCfgStore])
			log.Infof("jaj cluster: %s, localKube, remoteKube", cluster, localKube, remoteKube)
			if isCfgStore, _ := strconv.ParseBool(cluster.ObjectMeta.Annotations[ClusterPilotCfgStore]); isCfgStore {
				localKube = cluster.ObjectMeta.Annotations[ClusterAccessConfigFile]
			} else {
				remoteKube = cluster.ObjectMeta.Annotations[ClusterAccessConfigFile]
			}
		}
		if localKube == "" {
			log.Warnf("no config store is defined in the cluster registries")
			return "", "", nil
		}
		localKube = path.Join(dirname, localKube)
		if remoteKube != "" {
			remoteKube = path.Join(dirname, remoteKube)
		}
	}

	return localKube, remoteKube, nil
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
		clusters = append(clusters, &obj)

	}
	return
}
