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
	"istio.io/mixer/pkg/config"
)

// registry implements pkg/adapter/Registrar.
// registry is initialized in the constructor and is immutable thereafter.
// All registered builders must have unique names.
// It also implements builderFinder that manager uses.
type registry struct {
	buildersByName map[string]adapter.Builder
}

// newRegistry returns a new Builder registry.
func newRegistry(builders []adapter.RegisterFn) *registry {
	r := &registry{buildersByName: make(map[string]adapter.Builder)}
	for _, builder := range builders {
		builder(r)
	}
	glog.V(2).Infof("Available Builders: %v", r.buildersByName)

	// ensure interfaces are satisfied.
	// should be compiled out.
	var _ adapter.Registrar = r
	var _ builderFinder = r
	return r
}

// FindBuilder finds builder by name.
func (r *registry) FindBuilder(name string) (adapter.Builder, bool) {
	b, ok := r.buildersByName[name]
	return b, ok
}

// RegisterListChecker registers a new ListChecker builder.
func (r *registry) RegisterListChecker(list adapter.ListCheckerBuilder) {
	r.insert(list)
}

// RegisterDenyChecker registers a new DenyChecker builder.
func (r *registry) RegisterDenyChecker(deny adapter.DenyCheckerBuilder) {
	r.insert(deny)
}

// RegisterLogger registers a new Logger builder.
func (r *registry) RegisterLogger(logger adapter.LoggerBuilder) {
	r.insert(logger)
}

// RegisterAccessLogger registers a new Logger builder.
func (r *registry) RegisterAccessLogger(logger adapter.AccessLoggerBuilder) {
	r.insert(logger)
}

// RegisterQuota registers a new Quota builder.
func (r *registry) RegisterQuota(quota adapter.QuotaBuilder) {
	r.insert(quota)
}

// RegisterMetrics registers a new Metrics builder.
func (r *registry) RegisterMetrics(quota adapter.MetricsBuilder) {
	r.insert(quota)
}

func (r *registry) insert(b adapter.Builder) {
	if _, exists := r.buildersByName[b.Name()]; exists {
		panic(fmt.Errorf("attempting to register a builder with a name already in the registry: %s", b.Name()))
	}
	r.buildersByName[b.Name()] = b
}

// processBindings returns a fully constructed manager map and aspectSet given APIBinding.
func processBindings(bnds []aspect.APIBinding) (map[string]aspect.Manager, map[config.APIMethod]config.AspectSet) {
	r := make(map[string]aspect.Manager)
	as := make(map[config.APIMethod]config.AspectSet)

	// setup aspect sets for all methods
	for _, am := range config.APIMethods() {
		as[am] = config.AspectSet{}
	}
	for _, bnd := range bnds {
		r[bnd.Aspect.Kind()] = bnd.Aspect
		as[bnd.Method][bnd.Aspect.Kind()] = true
	}
	return r, as
}
