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

package meshcfg

import (
	"io/ioutil"
	"sync"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"

	"istio.io/pkg/filewatcher"

	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/pkg/config/event"
)

// FsSource is a event.InMemorySource implementation that reads mesh from file.
type FsSource struct {
	path string
	fw   filewatcher.FileWatcher

	inmemory *InMemorySource

	wg sync.WaitGroup

	// For overriding in tests
	yamlToJSON func(y []byte) ([]byte, error)
}

var _ event.Source = &FsSource{}

// NewFS returns a new mesh cache, based on watching a file.
func NewFS(path string) (*FsSource, error) {
	return newFS(path, yaml.YAMLToJSON)
}

// newFS returns a new mesh cache, based on watching a file.
func newFS(path string, yamlToJSON func(y []byte) ([]byte, error)) (*FsSource, error) {
	fw := filewatcher.NewWatcher()

	err := fw.Add(path)
	if err != nil {
		_ = fw.Close()
		return nil, err
	}

	c := &FsSource{
		path:       path,
		fw:         fw,
		inmemory:   NewInmemory(),
		yamlToJSON: yamlToJSON,
	}

	c.reload()

	// If we were not able to load mesh config, start with the default.
	if !c.inmemory.IsSynced() {
		scope.Processing.Infof("Unable to load up mesh config, using default values (path: %s)", path)
		c.inmemory.Set(Default())
	}

	c.wg.Add(1)
	go func() {
		ch := fw.Events(path)

		// Make sure the channel isn't nil. This can happen if the Stop function
		// is called before we get here.
		if ch != nil {
			for range ch {
				c.reload()
			}
		}
		c.wg.Done()
	}()

	return c, nil
}

// Start implements event.Source
func (c *FsSource) Start() {
	c.inmemory.Start()
}

// Stop implements event.Source
func (c *FsSource) Stop() {
	scope.Processing.Debugf("meshcfg.FsSource.Stop >>>")
	c.inmemory.Stop()

	// close the file watcher
	_ = c.fw.Close()

	// wait for the goroutine to be done
	c.wg.Wait()

	scope.Processing.Debugf("meshcfg.FsSource.Stop <<<")
}

// Dispatch implements event.Source
func (c *FsSource) Dispatch(h event.Handler) {
	c.inmemory.Dispatch(h)
}

func (c *FsSource) reload() {
	by, err := ioutil.ReadFile(c.path)
	if err != nil {
		scope.Processing.Errorf("Error loading mesh config (path: %s): %v", c.path, err)
		return
	}

	js, err := c.yamlToJSON(by)
	if err != nil {
		scope.Processing.Errorf("Error converting mesh config Yaml to JSON (path: %s): %v", c.path, err)
		return
	}

	cfg := Default()
	if err = jsonpb.UnmarshalString(string(js), cfg); err != nil {
		scope.Processing.Errorf("Error reading mesh config as JSON (path: %s): %v", c.path, err)
		return
	}

	c.inmemory.Set(cfg)
	scope.Processing.Infof("Reloaded mesh config (path: %s): \n%s\n", c.path, string(by))
}

// Close closes this cache.
func (c *FsSource) Close() error {
	return c.fw.Close()
}
