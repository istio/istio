// Copyright Istio Authors
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

package kubemesh

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/configmapwatcher"
	"istio.io/pkg/log"
)

// NewConfigMapWatcher creates a new Watcher for changes to the given ConfigMap.
func NewConfigMapWatcher(client kube.Client, namespace, name, key string, multiWatch bool, stop <-chan struct{}) *mesh.MultiWatcher {
	w := mesh.NewMultiWatcher(mesh.DefaultMeshConfig())
	c := configmapwatcher.NewController(client, namespace, name, func(cm *v1.ConfigMap) {
		meshNetworks, err := ReadNetworksConfigMap(cm, "meshNetworks")
		if err != nil {
			// Keep the last known config in case there's a misconfiguration issue.
			log.Warnf("failed to read mesh config from ConfigMap: %v", err)
			return
		}
		if meshNetworks != nil {
			w.SetNetworks(meshNetworks)
		}
		if multiWatch {
			meshConfig := meshConfigMapData(cm, key)
			w.HandleMeshConfigData(meshConfig)
			return
		}
		// Original behavior - just per-revision config
		meshConfig, err := ReadConfigMap(cm, key)
		if err != nil {
			// Keep the last known config in case there's a misconfiguration issue.
			log.Warnf("failed to read mesh config from ConfigMap: %v", err)
			return
		}
		w.HandleMeshConfig(meshConfig)
	})

	go c.Run(stop)

	// Ensure the ConfigMap is initially loaded if present.
	if !cache.WaitForCacheSync(stop, c.HasSynced) {
		log.Error("failed to wait for cache sync")
	}
	return w
}

func AddUserMeshConfig(client kube.Client, watcher mesh.Watcher, namespace, key, userMeshConfig string, stop chan struct{}) {
	c := configmapwatcher.NewController(client, namespace, userMeshConfig, func(cm *v1.ConfigMap) {
		meshConfig := meshConfigMapData(cm, key)
		watcher.HandleUserMeshConfig(meshConfig)
	})

	go c.Run(stop)
	if !cache.WaitForCacheSync(stop, c.HasSynced) {
		log.Error("failed to wait for cache sync")
	}
}

func meshConfigMapData(cm *v1.ConfigMap, key string) string {
	if cm == nil {
		return ""
	}

	cfgYaml, exists := cm.Data[key]
	if !exists {
		return ""
	}

	return cfgYaml
}

func ReadConfigMap(cm *v1.ConfigMap, key string) (*meshconfig.MeshConfig, error) {
	if cm == nil {
		log.Info("no ConfigMap found, using default MeshConfig config")
		return mesh.DefaultMeshConfig(), nil
	}

	cfgYaml, exists := cm.Data[key]
	if !exists {
		return nil, fmt.Errorf("missing ConfigMap key %q", key)
	}

	meshConfig, err := mesh.ApplyMeshConfigDefaults(cfgYaml)
	if err != nil {
		return nil, fmt.Errorf("failed reading MeshConfig config: %v. YAML:\n%s", err, cfgYaml)
	}

	log.Info("Loaded MeshConfig config from Kubernetes API server.")
	return meshConfig, nil
}

func ReadNetworksConfigMap(cm *v1.ConfigMap, key string) (*meshconfig.MeshNetworks, error) {
	if cm == nil {
		log.Info("no ConfigMap found, using existing MeshNetworks config")
		return nil, nil
	}

	cfgYaml, exists := cm.Data[key]
	if !exists {
		return nil, nil
	}

	meshNetworks, err := mesh.ParseMeshNetworks(cfgYaml)
	if err != nil {
		return nil, fmt.Errorf("failed reading MeshNetworks config: %v. YAML:\n%s", err, cfgYaml)
	}

	log.Info("Loaded MeshNetworks config from Kubernetes API server.")
	return meshNetworks, nil
}
