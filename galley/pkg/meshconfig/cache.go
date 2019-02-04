//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package meshconfig

import (
	"io"
	"io/ioutil"
	"sync"

	"github.com/ghodss/yaml"

	"github.com/gogo/protobuf/jsonpb"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("meshconfig", "meshconfig watcher/reader", 0)

// Cache is an interface for getting a cached copy of mesh.
type Cache interface {
	// Get returns the cached copy of mesh config.
	Get() v1alpha1.MeshConfig
}

// FsCache is a Cache implementation that reads mesh from file.
type FsCache struct {
	path string
	fw   filewatcher.FileWatcher

	cachedMutex sync.Mutex
	cached      v1alpha1.MeshConfig
}

var _ Cache = &FsCache{}
var _ io.Closer = &FsCache{}

// NewCacheFromFile returns a new mesh cache, based on watching a file.
func NewCacheFromFile(path string) (*FsCache, error) {
	fw := filewatcher.NewWatcher()

	err := fw.Add(path)
	if err != nil {
		return nil, err
	}

	c := &FsCache{
		path:   path,
		fw:     fw,
		cached: Default(),
	}

	c.reload()

	go func() {
		for range fw.Events(path) {
			c.reload()
		}
	}()

	return c, nil
}

// Get returns the cached copy of mesh config.
func (c *FsCache) Get() v1alpha1.MeshConfig {
	c.cachedMutex.Lock()
	defer c.cachedMutex.Unlock()
	return c.cached
}

func (c *FsCache) reload() {
	by, err := ioutil.ReadFile(c.path)
	if err != nil {
		scope.Errorf("Error loading mesh config (path: %s): %v", c.path, err)
		return
	}

	js, err := yaml.YAMLToJSON(by)
	if err != nil {
		scope.Errorf("Error converting mesh config Yaml to JSON: %v", err)
		return
	}

	cfg := Default()
	if err = jsonpb.UnmarshalString(string(js), &cfg); err != nil {
		scope.Errorf("Error reading mesh config as json: %v", err)
		return
	}

	c.cachedMutex.Lock()
	defer c.cachedMutex.Unlock()
	c.cached = cfg
	scope.Infof("Reloaded mesh config: \n%s\n", string(by))
}

// Close closes this cache.
func (c *FsCache) Close() error {
	return c.fw.Close()
}
