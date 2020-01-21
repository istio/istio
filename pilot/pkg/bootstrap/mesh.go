// Copyright 2019 Istio Authors
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

package bootstrap

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/util/gogoprotomarshal"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/mesh"
)

const (
	// configMapKey should match the expected MeshConfig file name
	configMapKey = "mesh"
)

// initMeshConfiguration creates the mesh in the pilotConfig from the input arguments.
func (s *Server) initMeshConfiguration(args *PilotArgs, fileWatcher filewatcher.FileWatcher) error {
	defer func() {
		if s.environment.Watcher != nil {
			meshdump, _ := gogoprotomarshal.ToJSONWithIndent(s.environment.Mesh(), "    ")
			log.Infof("mesh configuration: %s", meshdump)
			log.Infof("version: %s", version.Info.String())
			argsdump, _ := json.MarshalIndent(args, "", "   ")
			log.Infof("flags: %s", argsdump)
		}
	}()

	// If a config file was specified, use it.
	if args.MeshConfig != nil {
		s.environment.Watcher = mesh.NewFixedWatcher(args.MeshConfig)
		return nil
	}

	var err error
	s.environment.Watcher, err = mesh.NewWatcher(fileWatcher, args.Mesh.ConfigFile)
	if err == nil {
		return nil
	}

	// Config file either wasn't specified or failed to load - use a default mesh.
	meshConfig, err := getMeshConfig(s.kubeClient, kubecontroller.IstioNamespace, kubecontroller.IstioConfigMap)
	if err != nil {
		log.Warnf("failed to read the default mesh configuration: %v, from the %s config map in the %s namespace",
			err, kubecontroller.IstioConfigMap, kubecontroller.IstioNamespace)
		return err
	}

	// Allow some overrides for testing purposes.
	if args.Mesh.MixerAddress != "" {
		meshConfig.MixerCheckServer = args.Mesh.MixerAddress
		meshConfig.MixerReportServer = args.Mesh.MixerAddress
	}
	s.environment.Watcher = mesh.NewFixedWatcher(meshConfig)
	return nil
}

// initMeshNetworks loads the mesh networks configuration from the file provided
// in the args and add a watcher for changes in this file.
func (s *Server) initMeshNetworks(args *PilotArgs, fileWatcher filewatcher.FileWatcher) {
	if args.NetworksConfigFile == "" {
		log.Info("mesh networks configuration not provided")
	} else {
		var err error
		s.environment.NetworksWatcher, err = mesh.NewNetworksWatcher(fileWatcher, args.NetworksConfigFile)
		if err != nil {
			log.Infoa(err)
		}
	}

	if s.environment.NetworksWatcher == nil {
		log.Info("mesh networks configuration not provided")
		s.environment.NetworksWatcher = mesh.NewFixedNetworksWatcher(nil)
	}
}

// getMeshConfig fetches the ProxyMesh configuration from Kubernetes ConfigMap.
// Deprecated - does not watch !
func getMeshConfig(kube kubernetes.Interface, namespace, name string) (*meshconfig.MeshConfig, error) {
	if kube == nil {
		defaultMesh := mesh.DefaultMeshConfig()
		return &defaultMesh, nil
	}

	cfg, err := kube.CoreV1().ConfigMaps(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			defaultMesh := mesh.DefaultMeshConfig()
			return &defaultMesh, nil
		}
		return nil, err
	}

	// values in the data are strings, while proto might use a different data type.
	// therefore, we have to get a value by a key
	cfgYaml, exists := cfg.Data[configMapKey]
	if !exists {
		return nil, fmt.Errorf("missing configuration map key %q", configMapKey)
	}

	meshConfig, err := mesh.ApplyMeshConfigDefaults(cfgYaml)
	if err != nil {
		return nil, err
	}

	log.Warn("Loading default mesh config from K8S, no reload support.")
	return meshConfig, nil
}
