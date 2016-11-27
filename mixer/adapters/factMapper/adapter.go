// Copyright 2016 Google Inc.
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

package factMapper

import (
	"github.com/istio/mixer/adapters"
)

// AdapterConfig is used to configure a fact mapper adapter.
type AdapterConfig struct{}

type adapter struct{}

// NewFactMapperAdapter returns an Adapter
func NewFactMapperAdapter() adapters.Adapter {
	return adapter{}
}

func (a adapter) Name() string {
	return "FactMapper"
}

func (a adapter) Description() string {
	return "Performs config-driven mapping of facts to labels"
}

func (a adapter) DefaultConfig() adapters.AdapterConfig {
	return AdapterConfig{}
}

func (a adapter) Activate(config adapters.AdapterConfig) error {
	_ = config.(AdapterConfig)
	return nil
}

func (a adapter) Deactivate() {
}

func (a adapter) DefaultInstanceConfig() adapters.InstanceConfig {
	return InstanceConfig{}
}

func (a adapter) CreateInstance(config adapters.InstanceConfig) (adapters.Instance, error) {
	c := config.(InstanceConfig)
	return newInstance(&c)
}
