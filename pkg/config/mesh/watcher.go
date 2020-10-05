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

package mesh

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/davecgh/go-spew/spew"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/configmapwatcher"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
)

// Holder of a mesh configuration.
type Holder interface {
	Mesh() *meshconfig.MeshConfig
}

// Watcher is a Holder whose mesh config can be updated asynchronously.
type Watcher interface {
	Holder

	// AddMeshHandler registers a callback handler for changes to the mesh config.
	AddMeshHandler(func())
}

var _ Watcher = &watcher{}

type watcher struct {
	mutex    sync.Mutex
	handlers []func()
	mesh     *meshconfig.MeshConfig

	configMapWatcher *configmapwatcher.Controller
}

// NewFixedWatcher creates a new Watcher that always returns the given mesh config. It will never
// fire any events, since the config never changes.
func NewFixedWatcher(mesh *meshconfig.MeshConfig) Watcher {
	return &watcher{
		mesh: mesh,
	}
}

// NewWatcher creates a new Watcher for changes to the given mesh config file. Returns an error
// if the given file does not exist or failed during parsing.
func NewWatcher(fileWatcher filewatcher.FileWatcher, filename string) (Watcher, error) {
	meshConfig, err := ReadMeshConfig(filename)
	if err != nil {
		return nil, err
	}

	w := &watcher{
		mesh: meshConfig,
	}

	// Watch the config file for changes and reload if it got modified
	addFileWatcher(fileWatcher, filename, func() {
		// Reload the config file
		meshConfig, err = ReadMeshConfig(filename)
		if err != nil {
			log.Warnf("failed to read mesh configuration, using default: %v", err)
			return
		}
		w.handleMeshConfig(meshConfig)
	})
	return w, nil
}

// NewConfigMapWatcher creates a new Watcher for changes to the given ConfigMap.
func NewConfigMapWatcher(client kube.Client, namespace, name, key string) Watcher {
	defaultMesh := DefaultMeshConfig()
	w := &watcher{mesh: &defaultMesh}
	c := configmapwatcher.NewController(client, namespace, name, func() {
		meshConfig, err := w.readConfigMap(key)
		if err != nil {
			log.Warnf("failed to read mesh config from ConfigMap, using default: %v", err)
			meshConfig = &defaultMesh
		}
		w.handleMeshConfig(meshConfig)
	})
	w.configMapWatcher = c

	stop := make(chan struct{})
	go c.Run(stop)
	return w
}

// Mesh returns the latest mesh config.
func (w *watcher) Mesh() *meshconfig.MeshConfig {
	return (*meshconfig.MeshConfig)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&w.mesh))))
}

// AddMeshHandler registers a callback handler for changes to the mesh config.
func (w *watcher) AddMeshHandler(h func()) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.handlers = append(w.handlers, h)
}

func (w *watcher) handleMeshConfig(meshConfig *meshconfig.MeshConfig) {
	var handlers []func()

	w.mutex.Lock()
	if !reflect.DeepEqual(meshConfig, w.mesh) {
		log.Infof("mesh configuration updated to: %s", spew.Sdump(meshConfig))
		if !reflect.DeepEqual(meshConfig.ConfigSources, w.mesh.ConfigSources) {
			log.Info("mesh configuration sources have changed")
			// TODO Need to recreate or reload initConfigController()
		}

		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&w.mesh)), unsafe.Pointer(meshConfig))
		handlers = append(handlers, w.handlers...)
	}
	w.mutex.Unlock()

	for _, h := range handlers {
		h()
	}
}

func (w *watcher) readConfigMap(key string) (*meshconfig.MeshConfig, error) {
	// TODO(tbarrella): Refactor so that the ConfigMap is passed to the callback
	cm, err := w.configMapWatcher.Get()
	if err != nil {
		return nil, fmt.Errorf("couldn't get ConfigMap: %v", err)
	}
	if cm == nil {
		return nil, fmt.Errorf("no ConfigMap found")
	}

	cfgYaml, exists := cm.Data[key]
	if !exists {
		return nil, fmt.Errorf("missing ConfigMap key %q", key)
	}

	meshConfig, err := ApplyMeshConfigDefaults(cfgYaml)
	if err != nil {
		return nil, fmt.Errorf("failed reading mesh config: %v. YAML:\n%s", err, cfgYaml)
	}

	log.Info("Loaded mesh config from Kubernetes API server.")
	return meshConfig, nil
}
