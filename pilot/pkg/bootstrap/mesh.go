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
	"fmt"
	"reflect"
	"time"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	// configMapKey should match the expected MeshConfig file name
	configMapKey = "mesh"
)

func (s *Server) initMesh(args *PilotArgs) error {
	if err := s.initMeshConfiguration(args); err != nil {
		return fmt.Errorf("mesh config: %v", err)
	}
	if err := s.initMeshNetworks(args); err != nil {
		return fmt.Errorf("mesh networks: %v", err)
	}
	return nil
}

// initMeshConfiguration creates the mesh in the pilotConfig from the input arguments.
func (s *Server) initMeshConfiguration(args *PilotArgs) error {
	// If a config file was specified, use it.
	if args.MeshConfig != nil {
		s.mesh = args.MeshConfig
		return nil
	}
	var meshConfig *meshconfig.MeshConfig
	var err error

	if args.Mesh.ConfigFile != "" {
		meshConfig, err = cmd.ReadMeshConfig(args.Mesh.ConfigFile)
		if err != nil {
			log.Warnf("failed to read mesh configuration, using default: %v", err)
		}

		// Watch the config file for changes and reload if it got modified
		s.addFileWatcher(args.Mesh.ConfigFile, func() {
			// Reload the config file
			meshConfig, err = cmd.ReadMeshConfig(args.Mesh.ConfigFile)
			if err != nil {
				log.Warnf("failed to read mesh configuration, using default: %v", err)
				return
			}
			if !reflect.DeepEqual(meshConfig, s.mesh) {
				log.Infof("mesh configuration updated to: %s", spew.Sdump(meshConfig))
				if !reflect.DeepEqual(meshConfig.ConfigSources, s.mesh.ConfigSources) {
					log.Infof("mesh configuration sources have changed")
					//TODO Need to re-create or reload initConfigController()
				}
				s.mesh = meshConfig
				if s.EnvoyXdsServer != nil {
					s.EnvoyXdsServer.Env.Mesh = meshConfig
					s.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
				}
			}
		})
	}

	if meshConfig == nil {
		// Config file either wasn't specified or failed to load - use a default mesh.
		if meshConfig, err = getMeshConfig(s.kubeClient, kubecontroller.IstioNamespace, kubecontroller.IstioConfigMap); err != nil {
			log.Warnf("failed to read the default mesh configuration: %v, from the %s config map in the %s namespace",
				err, kubecontroller.IstioConfigMap, kubecontroller.IstioNamespace)
			return err
		}

		// Allow some overrides for testing purposes.
		if args.Mesh.MixerAddress != "" {
			meshConfig.MixerCheckServer = args.Mesh.MixerAddress
			meshConfig.MixerReportServer = args.Mesh.MixerAddress
		}
	}

	log.Infof("mesh configuration %s", spew.Sdump(meshConfig))
	log.Infof("version %s", version.Info.String())
	log.Infof("flags %s", spew.Sdump(args))

	s.mesh = meshConfig
	return nil
}

// initMeshNetworks loads the mesh networks configuration from the file provided
// in the args and add a watcher for changes in this file.
func (s *Server) initMeshNetworks(args *PilotArgs) error { //nolint: unparam
	if args.NetworksConfigFile == "" {
		log.Info("mesh networks configuration not provided")
		return nil
	}

	meshNetworks, err := cmd.ReadMeshNetworksConfig(args.NetworksConfigFile)
	if err != nil {
		log.Warnf("failed to read mesh networks configuration from %q: %v", args.NetworksConfigFile, err)
		return nil
	}
	log.Infof("mesh networks configuration %s", spew.Sdump(meshNetworks))
	util.ResolveHostsInNetworksConfig(meshNetworks)
	log.Infof("mesh networks configuration post-resolution %s", spew.Sdump(meshNetworks))
	s.meshNetworks = meshNetworks

	// Watch the networks config file for changes and reload if it got modified
	s.addFileWatcher(args.NetworksConfigFile, func() {
		// Reload the config file
		meshNetworks, err := cmd.ReadMeshNetworksConfig(args.NetworksConfigFile)
		if err != nil {
			log.Warnf("failed to read mesh networks configuration from %q", args.NetworksConfigFile)
			return
		}
		if !reflect.DeepEqual(meshNetworks, s.meshNetworks) {
			log.Infof("mesh networks configuration file updated to: %s", spew.Sdump(meshNetworks))
			util.ResolveHostsInNetworksConfig(meshNetworks)
			log.Infof("mesh networks configuration post-resolution %s", spew.Sdump(meshNetworks))
			s.meshNetworks = meshNetworks
			if s.kubeRegistry != nil {
				s.kubeRegistry.InitNetworkLookup(meshNetworks)
			}
			if s.multicluster != nil {
				s.multicluster.ReloadNetworkLookup(meshNetworks)
			}
			if s.EnvoyXdsServer != nil {
				s.EnvoyXdsServer.Env.MeshNetworks = meshNetworks
				s.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
			}
		}
	})

	return nil
}

// Add to the FileWatcher the provided file and execute the provided function
// on any change event for this file.
// Using a debouncing mechanism to avoid calling the callback multiple times
// per event.
func (s *Server) addFileWatcher(file string, callback func()) {
	_ = s.fileWatcher.Add(file)
	go func() {
		var timerC <-chan time.Time
		for {
			select {
			case <-timerC:
				timerC = nil
				callback()
			case <-s.fileWatcher.Events(file):
				// Use a timer to debounce configuration updates
				if timerC == nil {
					timerC = time.After(100 * time.Millisecond)
				}
			}
		}
	}()
}

// getMeshConfig fetches the ProxyMesh configuration from Kubernetes ConfigMap.
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
	return meshConfig, nil
}
