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

package adapterManager

import (
	"fmt"

	"github.com/golang/glog"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/config"
)

// BuilderInfo provides information about an individual builder.
type BuilderInfo struct {
	// Builder is the builder of interest.
	Builder adapter.Builder

	// Kinds specifies the aspect kinds that this builder is capable of handling.
	Kinds config.KindSet
}

// BuildersByName holds a set of builders indexed by their name.
type BuildersByName map[string]*BuilderInfo

// registry implements pkg/adapter/Registrar.
// registry is initialized in the constructor and is immutable thereafter.
// All registered builders must have unique names per aspect kind.
// It also implements builders that manager uses.
type registry struct {
	builders BuildersByName
}

// newRegistry returns a new Builder registry.
func newRegistry(builders []adapter.RegisterFn) *registry {
	r := &registry{make(BuildersByName)}
	for idx, builder := range builders {
		glog.V(3).Infof("Registering [%d] %#v", idx, builder)
		builder(r)
	}
	// ensure interfaces are satisfied.
	// should be compiled out.
	var _ adapter.Registrar = r
	var _ builderFinder = r
	return r
}

// BuilderMap returns the known builders, indexed by kind.
func BuilderMap(builders []adapter.RegisterFn) BuildersByName {
	return newRegistry(builders).builders
}

// FindBuilder finds builder by name.
func (r *registry) FindBuilder(name string) (b adapter.Builder, found bool) {
	var bi *BuilderInfo
	if bi, found = r.builders[name]; !found {
		return
	}
	return bi.Builder, true
}

func (r *registry) SupportedKinds(builder string) config.KindSet {
	var bi *BuilderInfo
	var found bool
	if bi, found = r.builders[builder]; !found {
		return 0
	}
	return bi.Kinds
}

// RegisterQuotasBuilder registers a new Quotas builder.
func (r *registry) RegisterQuotasBuilder(b adapter.QuotasBuilder) {
	r.insert(config.QuotasKind, b)
}

// RegisterAttributesGeneratorBuilder registers a new AttributesGenerator builder.
func (r *registry) RegisterAttributesGeneratorBuilder(b adapter.AttributesGeneratorBuilder) {
	r.insert(config.AttributesKind, b)
}

func (r *registry) insert(k config.Kind, b adapter.Builder) {
	bi := r.builders[b.Name()]
	if bi == nil {
		bi = &BuilderInfo{Builder: b}
		r.builders[b.Name()] = bi
	} else if bi.Builder != b {
		// panic only if 2 different builder objects are trying to identify by the
		// same Name.  2nd registration is ok so long as old and the new are same
		msg := fmt.Errorf("duplicate registration for '%s' : old = %v new = %v", b.Name(), bi.Builder, b)
		glog.Error(msg)
		panic(msg)
	}

	bi.Kinds = bi.Kinds.Set(k)

	if glog.V(1) {
		glog.Infof("Registered %s / %s", k, b.Name())
	}
}
