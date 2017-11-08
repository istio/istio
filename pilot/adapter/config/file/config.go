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
	"io/ioutil"
	"istio.io/istio/pilot/model"
	"fmt"
)

const (
	defaultNamespaceOverride = "default"
	defaultDomainOverride    = "cluster.local"
)

// Information for a single element of configuration stored in a file.
type FileConfig struct {
	Meta model.ConfigMeta
	File string
}

// A decorator around another ConfigStore that adds support for loading configuration elements from files.
type FileConfigStore interface {
	model.ConfigStore

	// Create a new configuration element from the specified file.
	CreateFromFile(config FileConfig) error

	GetForFile(fileConfig FileConfig) (config *model.Config, exists bool)
}

type fileConfigStore struct {
	model.ConfigStore

	namespaceOverride string
	domainOverride    string
}

// Creates a new file-based config store. This config store will use the namespace and domain from the read
// configurations directly (i.e. no override).
func NewFileConfigStore(configStore model.ConfigStore) FileConfigStore {
	return NewFileConfigStoreWithOverrides(configStore, "", "")
}

// Creates a new file-based config store. This config store will use default overrides for namespace and domain.
func NewFileConfigStoreWithDefaultOverrides(configStore model.ConfigStore) FileConfigStore {
	return NewFileConfigStoreWithOverrides(configStore, defaultNamespaceOverride, defaultDomainOverride)
}

// Creates a new file-based config store. This config store will use the provided overrides for namespace and domain.
func NewFileConfigStoreWithOverrides(configStore model.ConfigStore, namespaceOverride string, domainOverride string) FileConfigStore {
	return &fileConfigStore{
		ConfigStore:       configStore,
		namespaceOverride: namespaceOverride,
		domainOverride:    domainOverride}
}

func (r *fileConfigStore) namespace(config FileConfig) string {
	if r.namespaceOverride != "" {
		return r.namespaceOverride
	}
	return config.Meta.Namespace
}

func (r *fileConfigStore) domain(config FileConfig) string {
	if r.domainOverride != "" {
		return r.domainOverride
	}
	return config.Meta.Domain
}

func (r *fileConfigStore) GetForFile(fileConfig FileConfig) (config *model.Config, exists bool) {
	return r.Get(fileConfig.Meta.Type, fileConfig.Meta.Name, r.namespace(fileConfig))
}

func (r *fileConfigStore) CreateFromFile(config FileConfig) error {
	schema, ok := model.IstioConfigTypes.GetByType(config.Meta.Type)
	if !ok {
		return fmt.Errorf("missing schema for %q", config.Meta.Type)
	}
	content, err := ioutil.ReadFile(config.File)
	if err != nil {
		return err
	}
	spec, err := schema.FromYAML(string(content))
	if err != nil {
		return err
	}
	out := model.Config{
		ConfigMeta: config.Meta,
		Spec:       spec,
	}

	// Apply overrides if set.
	out.ConfigMeta.Namespace = r.namespace(config)
	out.ConfigMeta.Domain = r.domain(config)

	_, err = r.Create(out)
	if err != nil {
		return err
	}

	return nil
}
