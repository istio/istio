// Copyright 2017 Istio Authors.
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

// Package noopLegacy is an empty adapter implementing every aspect.
// WARNING: Not intended for actual use. This is a stand-in adapter used in benchmarking Mixer's adapter framework.
package noopLegacy

import (
	"istio.io/istio/mixer/pkg/adapter"
)

type (
	// Builder implements all adapter.*Builder interfaces
	Builder struct{ adapter.DefaultBuilder }
	aspect  struct{}
)

// Register registers the no-op adapter as every aspect.
func Register(r adapter.Registrar) {
	b := Builder{adapter.NewDefaultBuilder("noop builder", "", nil)}
	r.RegisterAttributesGeneratorBuilder(b)
	r.RegisterQuotasBuilder(b)
}

// BuildAttributesGenerator creates an adapter.AttributesGenerator instance
func (Builder) BuildAttributesGenerator(adapter.Env, adapter.Config) (adapter.AttributesGenerator, error) {
	return &aspect{}, nil
}
func (aspect) Generate(map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (aspect) Close() error { return nil }

// NewQuotasAspect creates an adapter.QuotasAspect instance
func (Builder) NewQuotasAspect(env adapter.Env, c adapter.Config, quotas map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return &aspect{}, nil
}
func (aspect) Alloc(adapter.QuotaArgsLegacy) (adapter.QuotaResultLegacy, error) {
	return adapter.QuotaResultLegacy{}, nil
}
func (aspect) AllocBestEffort(adapter.QuotaArgsLegacy) (adapter.QuotaResultLegacy, error) {
	return adapter.QuotaResultLegacy{}, nil
}
func (aspect) ReleaseBestEffort(adapter.QuotaArgsLegacy) (int64, error) { return 0, nil }
