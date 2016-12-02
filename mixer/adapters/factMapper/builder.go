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
	"istio.io/mixer/adapters"
)

// BuilderConfig is used to configure a builder.
type BuilderConfig struct{}

type builder struct{}

// NewBuilder returns a builder
func NewBuilder() adapters.Builder {
	return &builder{}
}

func (b *builder) Name() string {
	return "FactMapper"
}

func (b *builder) Description() string {
	return "Performs config-driven mapping of facts to labels."
}

func (b *builder) DefaultBuilderConfig() adapters.BuilderConfig {
	return &BuilderConfig{}
}

func (b *builder) ValidateBuilderConfig(config adapters.BuilderConfig) error {
	_ = config.(*BuilderConfig)
	return nil
}

func (b *builder) Configure(config adapters.BuilderConfig) error {
	return b.ValidateBuilderConfig(config)
}

func (b *builder) Close() error {
	return nil
}

func (b *builder) DefaultAdapterConfig() adapters.AdapterConfig {
	return &AdapterConfig{}
}

func (b *builder) ValidateAdapterConfig(config adapters.AdapterConfig) error {
	_ = config.(*AdapterConfig)
	return nil
}

func (b *builder) NewAdapter(config adapters.AdapterConfig) (adapters.Adapter, error) {
	if err := b.ValidateAdapterConfig(config); err != nil {
		return nil, err
	}
	c := config.(*AdapterConfig)
	return newAdapter(c)
}
