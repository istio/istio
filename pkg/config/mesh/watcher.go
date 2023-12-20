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
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
)

// Holder of a mesh configuration.
type Holder interface {
	Mesh() *meshconfig.MeshConfig
}

// Watcher is a Holder whose mesh config can be updated asynchronously.
type Watcher interface {
	Holder

	// AddMeshHandler registers a callback handler for changes to the mesh config.
	AddMeshHandler(h func()) *WatcherHandlerRegistration

	// DeleteMeshHandler unregisters a callback handler when remote cluster is removed.
	DeleteMeshHandler(registration *WatcherHandlerRegistration)

	// HandleUserMeshConfig keeps track of user mesh config overrides. These are merged with the standard
	// mesh config, which takes precedence.
	HandleUserMeshConfig(string)
}

// MultiWatcher is a struct wrapping the internal injector to let users know that both
type MultiWatcher struct {
	*internalWatcher
	internalNetworkWatcher
}

func NewMultiWatcher(config *meshconfig.MeshConfig) *MultiWatcher {
	iw := &internalWatcher{}
	iw.MeshConfig.Store(config)
	return &MultiWatcher{
		internalWatcher: iw,
	}
}

var _ Watcher = &internalWatcher{}

type internalWatcher struct {
	mutex    sync.Mutex
	handlers []*WatcherHandlerRegistration
	// Current merged mesh config
	MeshConfig atomic.Pointer[meshconfig.MeshConfig]

	userMeshConfig string
	revMeshConfig  string
}

// NewFixedWatcher creates a new Watcher that always returns the given mesh config. It will never
// fire any events, since the config never changes.
func NewFixedWatcher(mesh *meshconfig.MeshConfig) Watcher {
	iw := internalWatcher{}
	iw.MeshConfig.Store(mesh)
	return &iw
}

// NewFileWatcher creates a new Watcher for changes to the given mesh config file. Returns an error
// if the given file does not exist or failed during parsing.
func NewFileWatcher(fileWatcher filewatcher.FileWatcher, filename string, multiWatch bool) (Watcher, error) {
	meshConfigYaml, err := ReadMeshConfigData(filename)
	if err != nil {
		return nil, err
	}

	meshConfig, err := ApplyMeshConfigDefaults(meshConfigYaml)
	if err != nil {
		return nil, err
	}

	w := &internalWatcher{
		revMeshConfig: meshConfigYaml,
	}
	w.MeshConfig.Store(meshConfig)

	// Watch the config file for changes and reload if it got modified
	addFileWatcher(fileWatcher, filename, func() {
		if multiWatch {
			meshConfig, err := ReadMeshConfigData(filename)
			if err != nil {
				log.Warnf("failed to read mesh configuration, using default: %v", err)
				return
			}
			w.HandleMeshConfigData(meshConfig)
			return
		}
		// Reload the config file
		meshConfig, err = ReadMeshConfig(filename)
		if err != nil {
			log.Warnf("failed to read mesh configuration, using default: %v", err)
			return
		}
		w.HandleMeshConfig(meshConfig)
	})
	return w, nil
}

// Mesh returns the latest mesh config.
func (w *internalWatcher) Mesh() *meshconfig.MeshConfig {
	return w.MeshConfig.Load()
}

// AddMeshHandler registers a callback handler for changes to the mesh config.
func (w *internalWatcher) AddMeshHandler(h func()) *WatcherHandlerRegistration {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	handler := &WatcherHandlerRegistration{
		handler: h,
	}
	w.handlers = append(w.handlers, handler)
	return handler
}

func (w *internalWatcher) DeleteMeshHandler(registration *WatcherHandlerRegistration) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if len(w.handlers) == 0 {
		return
	}

	w.handlers = slices.FilterInPlace(w.handlers, func(handler *WatcherHandlerRegistration) bool {
		return handler != registration
	})
}

// HandleMeshConfigData keeps track of the standard mesh config. These are merged with the user
// mesh config, but takes precedence.
func (w *internalWatcher) HandleMeshConfigData(yaml string) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.revMeshConfig = yaml
	merged := w.merged()
	w.handleMeshConfigInternal(merged)
}

// HandleUserMeshConfig keeps track of user mesh config overrides. These are merged with the standard
// mesh config, which takes precedence.
func (w *internalWatcher) HandleUserMeshConfig(yaml string) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.userMeshConfig = yaml
	merged := w.merged()
	w.handleMeshConfigInternal(merged)
}

// merged returns the merged user and revision config.
func (w *internalWatcher) merged() *meshconfig.MeshConfig {
	mc := DefaultMeshConfig()
	if w.userMeshConfig != "" {
		mc1, err := ApplyMeshConfig(w.userMeshConfig, mc)
		if err != nil {
			log.Errorf("user config invalid, ignoring it %v %s", err, w.userMeshConfig)
		} else {
			mc = mc1
			log.Infof("Applied user config: %s", PrettyFormatOfMeshConfig(mc))
		}
	}
	if w.revMeshConfig != "" {
		mc1, err := ApplyMeshConfig(w.revMeshConfig, mc)
		if err != nil {
			log.Errorf("revision config invalid, ignoring it %v %s", err, w.userMeshConfig)
		} else {
			mc = mc1
			log.Infof("Applied revision mesh config: %s", PrettyFormatOfMeshConfig(mc))
		}
	}
	return mc
}

// HandleMeshConfig calls all handlers for a given mesh configuration update. This must be called
// with a lock on w.Mutex, or updates may be applied out of order.
func (w *internalWatcher) HandleMeshConfig(meshConfig *meshconfig.MeshConfig) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.handleMeshConfigInternal(meshConfig)
}

// handleMeshConfigInternal behaves the same as HandleMeshConfig but must be called under a lock
func (w *internalWatcher) handleMeshConfigInternal(meshConfig *meshconfig.MeshConfig) {
	var handlers []*WatcherHandlerRegistration

	current := w.MeshConfig.Load()
	if !reflect.DeepEqual(meshConfig, current) {
		log.Infof("mesh configuration updated to: %s", PrettyFormatOfMeshConfig(meshConfig))
		if !reflect.DeepEqual(meshConfig.ConfigSources, current.ConfigSources) {
			log.Info("mesh configuration sources have changed")
			// TODO Need to recreate or reload initConfigController()
		}

		w.MeshConfig.Store(meshConfig)
		handlers = append(handlers, w.handlers...)
	}

	// TODO hack: the first handler added is the ConfigPush, other handlers affect what will be pushed, so reversing iteration
	for i := len(handlers) - 1; i >= 0; i-- {
		handlers[i].handler()
	}
}

// Add to the FileWatcher the provided file and execute the provided function
// on any change event for this file.
// Using a debouncing mechanism to avoid calling the callback multiple times
// per event.
func addFileWatcher(fileWatcher filewatcher.FileWatcher, file string, callback func()) {
	_ = fileWatcher.Add(file)
	go func() {
		var timerC <-chan time.Time
		for {
			select {
			case <-timerC:
				timerC = nil
				callback()
			case <-fileWatcher.Events(file):
				// Use a timer to debounce configuration updates
				if timerC == nil {
					timerC = time.After(100 * time.Millisecond)
				}
			}
		}
	}()
}

func PrettyFormatOfMeshConfig(meshConfig *meshconfig.MeshConfig) string {
	meshConfigDump, _ := protomarshal.ToJSONWithIndent(meshConfig, "    ")
	return meshConfigDump
}
