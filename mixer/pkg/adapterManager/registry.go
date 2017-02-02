// Copyright 2017 Google Inc.
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

package adapterManager

import (
	"fmt"

	"github.com/golang/glog"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect"
)

// BuildersByName holds a set of builders of the same aspect kind, indexed by their name.
type BuildersByName map[string]adapter.Builder

// BuildersPerKind holds a set of builders, indexed by aspect kind.
type BuildersPerKind map[string]BuildersByName

// registry implements pkg/adapter/Registrar.
// registry is initialized in the constructor and is immutable thereafter.
// All registered builders must have unique names per aspect kind.
// It also implements builderFinder that manager uses.
type registry struct {
	builders BuildersPerKind
}

// newRegistry returns a new Builder registry.
func newRegistry(builders []adapter.RegisterFn) *registry {
	r := &registry{make(BuildersPerKind)}
	for idx, builder := range builders {
		glog.V(2).Infof("Registering [%d] %v", idx, builder)
		builder(r)
	}
	// ensure interfaces are satisfied.
	// should be compiled out.
	var _ adapter.Registrar = r
	var _ builderFinder = r
	return r
}

// BuilderMap returns the known builders, indexed by kind.
func BuilderMap(builders []adapter.RegisterFn) BuildersPerKind {
	r := newRegistry(builders)
	return r.builders
}

// FindBuilder finds builder by name.
func (r *registry) FindBuilder(kind string, name string) (adapter.Builder, bool) {
	m := r.builders[kind]
	b, ok := m[name]
	return b, ok
}

// RegisterListChecker registers a new ListChecker builder.
func (r *registry) RegisterListChecker(list adapter.ListCheckerBuilder) {
	r.insert(aspect.ListKind, list)
}

// RegisterDenyChecker registers a new DenyChecker builder.
func (r *registry) RegisterDenyChecker(deny adapter.DenyCheckerBuilder) {
	r.insert(aspect.DenyKind, deny)
}

// RegisterLogger registers a new Logger builder.
func (r *registry) RegisterLogger(logger adapter.LoggerBuilder) {
	r.insert(aspect.LogKind, logger)
}

// RegisterAccessLogger registers a new Logger builder.
func (r *registry) RegisterAccessLogger(logger adapter.AccessLoggerBuilder) {
	r.insert(aspect.AccessLogKind, logger)
}

// RegisterQuota registers a new Quota builder.
func (r *registry) RegisterQuota(quota adapter.QuotaBuilder) {
	r.insert(aspect.QuotaKind, quota)
}

// RegisterMetrics registers a new Metrics builder.
func (r *registry) RegisterMetrics(metrics adapter.MetricsBuilder) {
	r.insert(aspect.MetricKind, metrics)
}

func (r *registry) insert(kind string, b adapter.Builder) {
	glog.V(2).Infof("Registering %v:%v", kind, b)

	m := r.builders[kind]
	if m == nil {
		m = make(map[string]adapter.Builder)
		r.builders[kind] = m
	}

	if old, exists := m[b.Name()]; exists {
		panic(fmt.Errorf("duplicate registration for '%v:%s' : %v %v", kind, b.Name(), old, b))
	}

	m[b.Name()] = b
}
