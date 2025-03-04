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

package bootstrap

import (
	"os"

	"sigs.k8s.io/yaml"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/mesh/kubemesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/version"
)

const (
	// defaultMeshConfigMapName is the default name of the ConfigMap with the mesh config
	// The actual name can be different - use getMeshConfigMapName
	defaultMeshConfigMapName = "istio"
)

// initMeshConfiguration creates the mesh in the pilotConfig from the input arguments.
// Original/default behavior:
// - use the mounted file, if it exists.
// - use istio-REVISION if k8s is enabled
// - fallback to default
//
// If the 'SHARED_MESH_CONFIG' env is set (experimental feature in 1.10):
// - if a file exist, load it - will be merged
// - if istio-REVISION exists, will be used, even if the file is present.
// - the SHARED_MESH_CONFIG config map will also be loaded and merged.
func (s *Server) initMeshConfiguration(args *PilotArgs, fileWatcher filewatcher.FileWatcher) {
	log.Infof("initializing mesh configuration %v", args.MeshConfigFile)
	col := s.getMeshConfiguration(args, fileWatcher)
	col.AsCollection().WaitUntilSynced(s.internalStop)
	s.environment.Watcher = meshwatcher.ConfigAdapter(col)

	log.Infof("mesh configuration: %s", meshwatcher.PrettyFormatOfMeshConfig(s.environment.Mesh()))
	log.Infof("version: %s", version.Info.String())
	argsdump, _ := yaml.Marshal(args)
	log.Infof("flags: %s", argsdump)
}

// getMeshConfiguration builds up MeshConfig.
func (s *Server) getMeshConfiguration(args *PilotArgs, fileWatcher filewatcher.FileWatcher) krt.Singleton[meshwatcher.MeshConfigResource] {
	// We need to get mesh configuration up-front, before we start anything, so we use internalStop rather than scheduling a task to run
	// later.
	opts := krt.NewOptionsBuilder(s.internalStop, "", args.KrtDebugger)
	sources := s.getConfigurationSources(args, fileWatcher, args.MeshConfigFile, kubemesh.MeshConfigKey)
	if len(sources) == 0 {
		log.Warnf("Using default mesh - missing file %s and no k8s client", args.MeshConfigFile)
	}
	return meshwatcher.NewCollection(opts, sources...)
}

// initMeshNetworks loads the mesh networks configuration from the file provided
// in the args and add a watcher for changes in this file.
func (s *Server) initMeshNetworks(args *PilotArgs, fileWatcher filewatcher.FileWatcher) {
	log.Infof("initializing mesh networks configuration %v", args.NetworksConfigFile)
	col := s.getMeshNetworks(args, fileWatcher)
	col.AsCollection().WaitUntilSynced(s.internalStop)
	s.environment.NetworksWatcher = meshwatcher.NetworksAdapter(col)
	log.Infof("mesh networks configuration: %s", meshwatcher.PrettyFormatOfMeshNetworks(s.environment.MeshNetworks()))
}

func (s *Server) getMeshNetworks(args *PilotArgs, fileWatcher filewatcher.FileWatcher) krt.Singleton[meshwatcher.MeshNetworksResource] {
	// We need to get mesh networks up-front, before we start anything, so we use internalStop rather than scheduling a task to run
	// later.
	opts := krt.NewOptionsBuilder(s.internalStop, "", args.KrtDebugger)
	sources := s.getConfigurationSources(args, fileWatcher, args.NetworksConfigFile, kubemesh.MeshNetworksKey)
	if len(sources) == 0 {
		log.Warnf("Using default mesh networks - missing file %s and no k8s client", args.NetworksConfigFile)
	}
	return meshwatcher.NewNetworksCollection(opts, sources...)
}

func getMeshConfigMapName(revision string) string {
	name := defaultMeshConfigMapName
	if revision == "" || revision == "default" {
		return name
	}
	return name + "-" + revision
}

// getConfigurationSources builds the mesh sources. This can pull MeshConfig and Meshnetworks (based on file/configmap key)
// There are a variety of possible states:
// * default + file
// * default + file + configmap
// * default + configmap
// * default + configmap + configmap
// * default
func (s *Server) getConfigurationSources(args *PilotArgs, fileWatcher filewatcher.FileWatcher, file string, cmKey string) []meshwatcher.MeshConfigSource {
	opts := krt.NewOptionsBuilder(s.internalStop, "", args.KrtDebugger)
	// Watcher will be merging more than one mesh config source?
	var userMeshConfig *meshwatcher.MeshConfigSource
	if features.SharedMeshConfig != "" && s.kubeClient != nil {
		userMeshConfig = ptr.Of(kubemesh.NewConfigMapSource(s.kubeClient, args.Namespace, features.SharedMeshConfig, cmKey, opts))
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		fileSource, err := meshwatcher.NewFileSource(fileWatcher, file, opts)
		if err == nil {
			return toSources(fileSource, userMeshConfig)
		}
	}

	if s.kubeClient == nil {
		return nil
	}
	configMapName := getMeshConfigMapName(args.Revision)
	primary := kubemesh.NewConfigMapSource(s.kubeClient, args.Namespace, configMapName, cmKey, opts)
	return toSources(primary, userMeshConfig)
}

func toSources(base meshwatcher.MeshConfigSource, user *meshwatcher.MeshConfigSource) []meshwatcher.MeshConfigSource {
	if user != nil {
		// User configuration is applied first
		return []meshwatcher.MeshConfigSource{*user, base}
	}
	return []meshwatcher.MeshConfigSource{base}
}
