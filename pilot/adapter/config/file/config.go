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
	"fmt"
	"io/ioutil"

	"istio.io/istio/pilot/model"
)

const (
	defaultNamespace = "default"
	defaultDomain    = "cluster.local"
)

// Information for a single element of configuration stored in a file.
type ConfigRef interface {
	Meta() *model.ConfigMeta
	FilePath() string
}

type configRef struct {
	meta     *model.ConfigMeta
	filePath string
}

func (r *configRef) Meta() *model.ConfigMeta {
	return r.meta
}

func (r *configRef) FilePath() string {
	return r.filePath
}

// Creates a new ConfigRef with the given properties.
func NewConfigRef(schemaType string, name string, namespace string, domain string, filePath string) ConfigRef {
	return &configRef{
		meta: &model.ConfigMeta{
			Name:      name,
			Type:      schemaType,
			Namespace: namespace,
			Domain:    domain},
		filePath: filePath,
	}
}

// Creates a new ConfigRef with a default namespace and domain.
func NewConfigRefWithDefaults(schemaType string, name string, file string) ConfigRef {
	return NewConfigRef(schemaType, name, defaultNamespace, defaultDomain, file)
}

// A decorator around another ConfigStore that adds support for loading configuration elements from files.
type ConfigStore interface {
	model.ConfigStore

	// Create a new configuration element from the specified file.
	CreateFromFile(config ConfigRef) error
}

type configStore struct {
	model.ConfigStore
}

// Creates a new file-based config store.
func NewConfigStore(store model.ConfigStore) ConfigStore {
	return &configStore{store}
}

func (r *configStore) CreateFromFile(config ConfigRef) error {
	schema, ok := model.IstioConfigTypes.GetByType(config.Meta().Type)
	if !ok {
		return fmt.Errorf("missing schema for %q", config.Meta().Type)
	}
	content, err := ioutil.ReadFile(config.FilePath())
	if err != nil {
		return err
	}
	spec, err := schema.FromYAML(string(content))
	if err != nil {
		return err
	}
	out := model.Config{
		ConfigMeta: *config.Meta(),
		Spec:       spec,
	}

	_, err = r.Create(out)
	if err != nil {
		return err
	}

	return nil
}
