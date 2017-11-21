// Copyright 2017 Istio Authors
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

package file

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/golang/glog"

	"istio.io/istio/pilot/adapter/config/crd"
	"istio.io/istio/pilot/model"
)

const (
	defaultDuration = time.Second / 2
)

var (
	supportedExtensions = map[string]bool{
		".yaml": true,
		".yml":  true,
	}
)

// compareIds compares the IDs (i.e. Namespace, Type, and Name) of the two configs and returns
// 0 if a == b, -1 if a < b, and 1 if a > b. Used for sorting config arrays.
func compareIds(a, b *model.Config) int {
	// Compare Namespace
	if v := strings.Compare(a.Namespace, b.Namespace); v != 0 {
		return v
	}
	// Compare Type
	if v := strings.Compare(a.Type, b.Type); v != 0 {
		return v
	}
	// Compare Name
	return strings.Compare(a.Name, b.Name)
}

// byKey is an array of config objects that is capable or sorting by Namespace, Type, and Name.
type byKey []*model.Config

func (rs byKey) Len() int {
	return len(rs)
}

func (rs byKey) Swap(i, j int) {
	rs[i], rs[j] = rs[j], rs[i]
}

func (rs byKey) Less(i, j int) bool {
	return compareIds(rs[i], rs[j]) < 0
}

// Monitor will monitor a directory containing yaml files for changes, updating a ConfigStore as changes are
// detected.
type Monitor struct {
	store         model.ConfigStore
	root          string
	types         map[string]bool
	checkDuration time.Duration
	configs       []*model.Config
}

// NewMonitor creates a new config store that monitors files under the given root directory for changes to config.
// If no types are provided (nil or empty), all IstioConfigTypes will be allowed.
func NewMonitor(delegateStore model.ConfigStore, rootDirectory string, types []string) *Monitor {
	monitor := &Monitor{
		store:         delegateStore,
		root:          rootDirectory,
		types:         map[string]bool{},
		checkDuration: defaultDuration,
	}

	if len(types) == 0 {
		types = model.IstioConfigTypes.Types()
	}

	for _, k := range types {
		if schema, ok := model.IstioConfigTypes.GetByType(k); ok {
			monitor.types[schema.Type] = true
		}
	}
	return monitor
}

// Start launches a thread that monitors files under the root directory, updating the underlying config
// store when changes are detected.
func (m *Monitor) Start(ctx context.Context) {
	m.checkAndUpdate()
	tick := time.NewTicker(m.checkDuration)
	go func() {
		for {
			select {
			case <-ctx.Done():
				tick.Stop()
				return
			case <-tick.C:
				m.checkAndUpdate()
			}
		}
	}()
}

// parseInputs is identical to crd.ParseInputs, except that it returns an array of config pointers.
func parseInputs(data []byte) ([]*model.Config, error) {
	configs, _, err := crd.ParseInputs(string(data))

	// Convert to an array of pointers.
	refs := make([]*model.Config, len(configs))
	for i := range configs {
		refs[i] = &configs[i]
	}
	return refs, err
}

// ParseYaml is a utility method used for testing purposes. It parses the given yaml file into an array of config
// objects sorted them according to their keys.
func ParseYaml(data []byte) ([]*model.Config, error) {
	configs, err := parseInputs(data)
	sort.Sort(byKey(configs))
	return configs, err
}

func (m *Monitor) checkAndUpdate() {
	// Read all of the files under the directory and sort them based on their type, name, and namespace.
	newConfigs := m.readFiles()

	// Compare the new list to the previous one and detect changes.
	oldLen := len(m.configs)
	newLen := len(newConfigs)
	oldIndex, newIndex := 0, 0
	for oldIndex < oldLen && newIndex < newLen {
		oldConfig := m.configs[oldIndex]
		newConfig := newConfigs[newIndex]
		if v := compareIds(oldConfig, newConfig); v < 0 {
			m.deleteConfig(oldConfig)
			oldIndex++
		} else if v > 0 {
			m.createConfig(newConfig)
			newIndex++
		} else {
			if !reflect.DeepEqual(oldConfig, newConfig) {
				m.updateConfig(newConfig)
			}
			oldIndex++
			newIndex++
		}
	}

	// Detect remaining deletions
	for ; oldIndex < oldLen; oldIndex++ {
		m.deleteConfig(m.configs[oldIndex])
	}

	// Detect remaining additions
	for ; newIndex < newLen; newIndex++ {
		m.createConfig(newConfigs[newIndex])
	}

	// Save the updated list.
	m.configs = newConfigs
}

func (m *Monitor) createConfig(c *model.Config) {
	if _, err := m.store.Create(*c); err != nil {
		glog.Warningf("Failed to create config (%m): %v ", *c, err)
	}
}

func (m *Monitor) updateConfig(c *model.Config) {
	// Set the resource version based on the existing config.
	if prev, exists := m.store.Get(c.Type, c.Name, c.Namespace); exists {
		c.ResourceVersion = prev.ResourceVersion
	}

	if _, err := m.store.Update(*c); err != nil {
		glog.Warningf("Failed to update config (%m): %v ", *c, err)
	}
}

func (m *Monitor) deleteConfig(c *model.Config) {
	if err := m.store.Delete(c.Type, c.Name, c.Namespace); err != nil {
		glog.Warningf("Failed to delete config (%m): %v ", *c, err)
	}
}

func (m *Monitor) readFiles() []*model.Config {
	var result []*model.Config

	err := filepath.Walk(m.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		} else if !supportedExtensions[filepath.Ext(path)] || (info.Mode()&os.ModeType) != 0 {
			return nil
		}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			glog.Warningf("Failed to read %s: %v", path, err)
			return err
		}
		configs, err := parseInputs(data)
		if err != nil {
			glog.Warningf("Failed to parse %s: %v", path, err)
			return err
		}

		// Filter any unsupported types before appending to the result.
		for _, cfg := range configs {
			if !m.types[cfg.Type] {
				continue
			}
			result = append(result, cfg)
		}
		return nil
	})
	if err != nil {
		glog.Warningf("failure during filepath.Walk: %v", err)
	}

	// Sort by the config IDs.
	sort.Sort(byKey(result))
	return result
}
