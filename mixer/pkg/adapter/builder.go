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

package adapter

// DefaultBuilder provides default adapter builder behavior for adapters.
// TODO: rename to DefaultFactory
type DefaultBuilder struct {
	name string
	desc string
	conf Config
}

// NewDefaultBuilder creates a new builder struct with the supplied params.
func NewDefaultBuilder(name, desc string, config Config) DefaultBuilder {
	return DefaultBuilder{name, desc, config}
}

// Name returns the official name of the adapter produced by this builder.
func (b DefaultBuilder) Name() string { return b.name }

// Description returns the official description of the adapter produced by this builder.
func (b DefaultBuilder) Description() string { return b.desc }

// DefaultConfig returns a default configuration struct for the adapters built by this builder.
func (b DefaultBuilder) DefaultConfig() Config { return b.conf }

// ValidateConfig determines whether the given configuration meets all correctness requirements.
func (DefaultBuilder) ValidateConfig(c Config) (ce *ConfigErrors) { return nil }

// Close provides a hook for behavior used to cleanup a builder when it is removed.
func (DefaultBuilder) Close() error { return nil }
