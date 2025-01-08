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
	"encoding/json"
	"os"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/kubemesh"
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
	// configMapKey should match the expected MeshConfig file name
	configMapKey = "mesh"
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
	defer func() {
		if s.environment.Watcher != nil {
			log.Infof("mesh configuration: %s", mesh.PrettyFormatOfMeshConfig(s.environment.Mesh()))
			log.Infof("version: %s", version.Info.String())
			argsdump, _ := json.MarshalIndent(args, "", "   ")
			log.Infof("flags: %s", argsdump)
		}
	}()
	col := s.getMeshConfiguration(args, fileWatcher)
	col.AsCollection().Synced().WaitUntilSynced(s.internalStop)
	s.environment.Watcher = mesh.Adapter(col)
}

func (s *Server) getMeshConfiguration(args *PilotArgs, fileWatcher filewatcher.FileWatcher) krt.Singleton[mesh.MeshConfigResource] {
	// Watcher will be merging more than one mesh config source?
	var userMeshConfig *mesh.MeshConfigSource
	if features.SharedMeshConfig != "" && s.kubeClient != nil {
		userMeshConfig = ptr.Of(kubemesh.NewConfigMapSource(s.kubeClient, args.Namespace, features.SharedMeshConfig, s.internalStop))
	}
	if _, err := os.Stat(args.MeshConfigFile); !os.IsNotExist(err) {
		fileSource, err := mesh.NewFileSource(fileWatcher, args.MeshConfigFile, s.internalStop)
		if err == nil {
			return mesh.NewCollection(&fileSource, userMeshConfig, s.internalStop)
		}
	}

	if s.kubeClient == nil {
		// Use a default mesh.
		log.Warnf("Using default mesh - missing file %s and no k8s client", args.MeshConfigFile)
		return mesh.NewCollection(nil, nil, s.internalStop)
	}
	configMapName := getMeshConfigMapName(args.Revision)
	primary := kubemesh.NewConfigMapSource(s.kubeClient, args.Namespace, configMapName, s.internalStop)
	return mesh.NewCollection(&primary, userMeshConfig, s.internalStop)
}

// initMeshNetworks loads the mesh networks configuration from the file provided
// in the args and add a watcher for changes in this file.
func (s *Server) initMeshNetworks(args *PilotArgs, fileWatcher filewatcher.FileWatcher) {
	if s.environment.NetworksWatcher != nil {
		return
	}
	log.Info("initializing mesh networks")
	if args.NetworksConfigFile != "" {
		var err error
		s.environment.NetworksWatcher, err = mesh.NewNetworksWatcher(fileWatcher, args.NetworksConfigFile)
		if err != nil {
			log.Info(err)
		}
	}

	if s.environment.NetworksWatcher == nil {
		log.Info("mesh networks configuration not provided")
		s.environment.NetworksWatcher = mesh.NewFixedNetworksWatcher(nil)
	}
}

func getMeshConfigMapName(revision string) string {
	name := defaultMeshConfigMapName
	if revision == "" || revision == "default" {
		return name
	}
	return name + "-" + revision
}
