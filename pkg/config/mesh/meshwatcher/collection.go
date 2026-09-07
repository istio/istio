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

package meshwatcher

import (
	"os"
	"path"
	"strings"
	"sync/atomic"

	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/kube/krt"
	krtfiles "istio.io/istio/pkg/kube/krt/files"
	"istio.io/istio/pkg/log"
)

// MeshConfigSource provides a named mesh config input.
type MeshConfigSource struct {
	krt.Singleton[string]
	Name string
}

// NewFileSource creates a MeshConfigSource from a file. The file must exist.
func NewFileSource(fileWatcher filewatcher.FileWatcher, filename string, opts krt.OptionsBuilder) (MeshConfigSource, error) {
	sourceName := "Mesh_File_" + path.Base(filename)
	source, err := krtfiles.NewFileSingleton[string](fileWatcher, filename, func(filename string) (string, error) {
		b, err := os.ReadFile(filename)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}, opts.WithName(sourceName)...)
	if err != nil {
		return MeshConfigSource{}, err
	}
	return MeshConfigSource{Singleton: source, Name: sourceName}, nil
}

// NewCollection builds a new mesh config built by applying the provided sources.
// Sources are applied in order (example: default < sources[0] < sources[1]).
func NewCollection(opts krt.OptionsBuilder, sources ...MeshConfigSource) krt.Singleton[MeshConfigResource] {
	if len(sources) > 2 {
		// There is no real reason for this other than to enforce we don't accidentally put more sources
		panic("currently only 2 sources are supported")
	}
	var hasValidConfig atomic.Bool
	return krt.NewSingleton[MeshConfigResource](
		func(ctx krt.HandlerContext) *MeshConfigResource {
			meshCfg := mesh.DefaultMeshConfig()
			appliedConfig := false
			appliedSource := ""
			failedSources := []string{}

			for _, attempt := range sources {
				s := krt.FetchOne(ctx, attempt.AsCollection())
				if s == nil {
					// Source specified but not giving us any data
					// Practically, this means the configmap doesn't exist -- for the file source, we only build it if the file exists
					// It is valid to run with no configmap, so we just log at debug level.
					log.Debugf("mesh configuration source missing")
					continue
				}
				n, err := mesh.ApplyMeshConfig(*s, meshCfg)
				if err != nil {
					meshConfigLoadErrors.Increment()
					failedSources = append(failedSources, attempt.Name)
					// For backwards compatibility, keep inconsistent behavior
					// TODO(https://github.com/istio/istio/issues/54615) align this.
					if len(sources) == 1 {
						if hasValidConfig.Load() {
							log.Errorf("invalid mesh config from %s, using last known state: %v", attempt.Name, err)
						} else {
							log.Errorf("invalid mesh config from %s, no last known state, using default mesh configuration: %v", attempt.Name, err)
						}
						// We never want a nil mesh config. If it fails, we discard the result but allow falling back to the
						// default if there is no last known state.
						// We may consider failing hard on startup instead of silently ignoring errors.
						ctx.DiscardResult()
						return &MeshConfigResource{mesh.DefaultMeshConfig()}
					}
					log.Errorf("invalid mesh config from %s, ignoring: %v", attempt.Name, err)
					continue
				}
				meshCfg = n
				appliedConfig = true
				appliedSource = attempt.Name
			}
			if len(failedSources) > 0 {
				failed := strings.Join(failedSources, ", ")
				if appliedConfig {
					log.Warnf("mesh configuration loaded with errors from %s, using configuration from %s along with defaults", failed, appliedSource)
				} else {
					log.Warnf("mesh configuration loaded with errors from %s, using default mesh configuration", failed)
				}
			}
			hasValidConfig.Store(appliedConfig)
			return &MeshConfigResource{meshCfg}
		}, opts.WithName("MeshConfig")...,
	)
}

// NewNetworksCollection builds a new meshnetworks config built by applying the provided sources.
// Sources are applied in order (example: default < sources[0] < sources[1]).
func NewNetworksCollection(opts krt.OptionsBuilder, sources ...MeshConfigSource) krt.Singleton[MeshNetworksResource] {
	if len(sources) > 2 {
		// There is no real reason for this other than to enforce we don't accidentally put more sources
		panic("currently only 2 sources are supported")
	}
	return krt.NewSingleton[MeshNetworksResource](
		func(ctx krt.HandlerContext) *MeshNetworksResource {
			for _, attempt := range sources {
				if s := krt.FetchOne(ctx, attempt.AsCollection()); s != nil {
					n, err := mesh.ParseMeshNetworks(*s)
					if err != nil {
						log.Warnf("invalid mesh networks, using last known state: %v", err)
						ctx.DiscardResult()
						return &MeshNetworksResource{mesh.DefaultMeshNetworks()}
					}
					return &MeshNetworksResource{n}
				}
			}
			return &MeshNetworksResource{nil}
		}, opts.WithName("MeshNetworks")...,
	)
}
