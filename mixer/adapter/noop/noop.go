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

// Package noop is an empty adapter implementing every aspect.
// WARNING: Not intended for actual use. This is a stand-in adapter used in benchmarking Mixer's adapter framework.
package noop

import (
	"github.com/gogo/protobuf/types"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/status"
)

type (
	builder struct{}
	aspect  struct{}
)

// Register registers the no-op adapter as every aspect.
func Register(r adapter.Registrar) {
	r.RegisterListsBuilder(builder{})
	r.RegisterDenialsBuilder(builder{})
	r.RegisterApplicationLogsBuilder(builder{})
	r.RegisterAccessLogsBuilder(builder{})
	r.RegisterAttributesGeneratorBuilder(builder{})
	r.RegisterQuotasBuilder(builder{})
	r.RegisterMetricsBuilder(builder{})
}

func (builder) Name() string                                          { return "no-op" }
func (builder) Close() error                                          { return nil }
func (builder) Description() string                                   { return "an adapter that does nothing" }
func (builder) DefaultConfig() (c adapter.Config)                     { return &types.Empty{} }
func (builder) ValidateConfig(c adapter.Config) *adapter.ConfigErrors { return nil }

func (aspect) Close() error { return nil }

func (builder) BuildAttributesGenerator(adapter.Env, adapter.Config) (adapter.AttributesGenerator, error) {
	return &aspect{}, nil
}
func (aspect) Generate(map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (builder) NewAccessLogsAspect(adapter.Env, adapter.Config) (adapter.AccessLogsAspect, error) {
	return &aspect{}, nil
}
func (aspect) LogAccess([]adapter.LogEntry) error { return nil }

func (builder) NewApplicationLogsAspect(adapter.Env, adapter.Config) (adapter.ApplicationLogsAspect, error) {
	return &aspect{}, nil
}
func (aspect) Log([]adapter.LogEntry) error { return nil }

func (builder) NewDenialsAspect(adapter.Env, adapter.Config) (adapter.DenialsAspect, error) {
	return &aspect{}, nil
}
func (aspect) Deny() rpc.Status { return status.New(rpc.FAILED_PRECONDITION) }

func (builder) NewListsAspect(adapter.Env, adapter.Config) (adapter.ListsAspect, error) {
	return &aspect{}, nil
}
func (aspect) CheckList(symbol string) (bool, error) { return false, nil }

func (builder) NewMetricsAspect(adapter.Env, adapter.Config, map[string]*adapter.MetricDefinition) (adapter.MetricsAspect, error) {
	return &aspect{}, nil
}
func (aspect) Record([]adapter.Value) error { return nil }

func (builder) NewQuotasAspect(adapter.Env, adapter.Config, map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return &aspect{}, nil
}
func (aspect) Alloc(adapter.QuotaArgs) (adapter.QuotaResult, error) { return adapter.QuotaResult{}, nil }
func (aspect) AllocBestEffort(adapter.QuotaArgs) (adapter.QuotaResult, error) {
	return adapter.QuotaResult{}, nil
}
func (aspect) ReleaseBestEffort(adapter.QuotaArgs) (int64, error) { return 0, nil }
