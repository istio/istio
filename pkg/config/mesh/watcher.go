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
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/davecgh/go-spew/spew"

	meshconfig "istio.io/api/mesh/v1alpha1"
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

		var handlers []func()

		w.mutex.Lock()
		if !reflect.DeepEqual(meshConfig, w.mesh) {
			log.Infof("mesh configuration updated to: %s", spew.Sdump(meshConfig))
			if !reflect.DeepEqual(meshConfig.ConfigSources, w.mesh.ConfigSources) {
				log.Infof("mesh configuration sources have changed")
				//TODO Need to re-create or reload initConfigController()
			}

			// Store the new mesh.
			atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&w.mesh)), unsafe.Pointer(meshConfig))
			handlers = append([]func(){}, w.handlers...)
		}
		w.mutex.Unlock()

		// Notify the handlers of the change.
		for _, h := range handlers {
			h()
		}
	})
	return w, nil
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
