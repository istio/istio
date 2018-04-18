// Copyright 2018 Istio Authors
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

package monitor

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

var (
	supportedExtensions = map[string]bool{
		".yaml": true,
		".yml":  true,
	}
)

// FileSnapshot holds a reference to a file directory that contains crd
// config and filter criteria for which of those configs will be parsed.
type FileSnapshot struct {
	root             string
	configTypeFilter map[string]bool
}

// NewFileSnapshot returns a snapshotter.
// If no types are provided in the descriptor, all IstioConfigTypes will be allowed.
func NewFileSnapshot(root string, descriptor model.ConfigDescriptor) *FileSnapshot {
	snapshot := &FileSnapshot{
		root:             root,
		configTypeFilter: make(map[string]bool),
	}

	types := descriptor.Types()
	if len(types) == 0 {
		types = model.IstioConfigTypes.Types()
	}

	for _, k := range types {
		if schema, ok := model.IstioConfigTypes.GetByType(k); ok {
			snapshot.configTypeFilter[schema.Type] = true
		}
	}

	return snapshot
}

// ReadConfigFiles parses files in the root directory and returns a sorted slice of
// eligible model.Config. This can be used as a configFunc when creating a Monitor.
func (f *FileSnapshot) ReadConfigFiles() ([]*model.Config, error) {
	var result []*model.Config

	err := filepath.Walk(f.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		} else if !supportedExtensions[filepath.Ext(path)] || (info.Mode()&os.ModeType) != 0 {
			return nil
		}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			log.Warnf("Failed to read %s: %v", path, err)
			return err
		}
		configs, err := parseInputs(data)
		if err != nil {
			log.Warnf("Failed to parse %s: %v", path, err)
			return err
		}

		// Filter any unsupported types before appending to the result.
		for _, cfg := range configs {
			if !f.configTypeFilter[cfg.Type] {
				continue
			}
			result = append(result, cfg)
		}
		return nil
	})
	if err != nil {
		log.Warnf("failure during filepath.Walk: %v", err)
	}

	// Sort by the config IDs.
	sort.Sort(byKey(result))
	return result, err
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
