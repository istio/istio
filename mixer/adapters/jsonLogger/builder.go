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

package jsonLogger

import (
	"istio.io/mixer/adapters"
)

const (
	name = "istio.io/mixer/loggers/jsonLogger"
	desc = "Writes adapters.LogEntrys to os.Stdout with JSON encoding"
)

type (
	builder struct{}
)

// TODO: consider making destination configurable (os.Stdout v os.Stderr)

// NewBuilder returns the builder for the default logging adapter.
func NewBuilder() adapters.Builder { return &builder{} }

func (b *builder) Close() error { return nil }

func (b builder) Name() string { return name }

func (b builder) Description() string { return desc }

func (b builder) DefaultBuilderConfig() adapters.BuilderConfig { return &struct{}{} }

func (b builder) ValidateBuilderConfig(c adapters.BuilderConfig) error { return nil }

func (b builder) Configure(c adapters.BuilderConfig) error { return nil }

func (b builder) DefaultAdapterConfig() adapters.AdapterConfig { return config{} }

func (b builder) ValidateAdapterConfig(c adapters.AdapterConfig) error { return nil }

func (b builder) NewAdapter(c adapters.AdapterConfig) (adapters.Adapter, error) {
	return newLogger(c)
}
