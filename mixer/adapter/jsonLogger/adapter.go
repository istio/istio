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
	"istio.io/mixer/pkg/adapter"
)

const (
	name = "istio.io/mixer/loggers/jsonLogger"
	desc = "Writes adapter.LogEntrys to os.Stdout with JSON encoding"
)

type (
	adapterState struct{}
)

// TODO: consider making destination configurable (os.Stdout v os.Stderr)

// NewAdapter returns the adapter for the default logging adapter.
func NewAdapter() adapter.Adapter { return &adapterState{} }

func (a *adapterState) Close() error { return nil }

func (a *adapterState) Name() string { return name }

func (a *adapterState) Description() string { return desc }

func (a *adapterState) DefaultAdapterConfig() adapter.AdapterConfig { return &struct{}{} }

func (a *adapterState) ValidateAdapterConfig(c adapter.AdapterConfig) error { return nil }

func (a *adapterState) Configure(c adapter.AdapterConfig) error { return nil }

func (a *adapterState) DefaultAspectConfig() adapter.AspectConfig { return config{} }

func (a *adapterState) ValidateAspectConfig(c adapter.AspectConfig) error { return nil }

func (a *adapterState) NewAspect(c adapter.AspectConfig) (adapter.Aspect, error) {
	return newLogger(c)
}
