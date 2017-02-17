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

type builderInfo struct {
	Builder adapter.Builder
	Kinds   []string
}

// BuildersByName holds a set of builders of the same aspect kind, indexed by their name.
type BuildersByName map[string]*builderInfo

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
	var bi *builderInfo
	if bi, found = r.builders[name]; !found {
		return
	}
	return bi.Builder, true
}

func (r *registry) SupportedKinds(name string) []string {
	var bi *builderInfo
	var found bool
	if bi, found = r.builders[name]; !found {
		return nil
	}
	return bi.Kinds
}

// RegisterListsBuilder registers a new ListChecker builder.
func (r *registry) RegisterListsBuilder(b adapter.ListsBuilder) {
	r.insert(aspect.ListsKind, b)
}

// RegisterDenialsBuilder registers a new DenyChecker builder.
func (r *registry) RegisterDenialsBuilder(b adapter.DenialsBuilder) {
	r.insert(aspect.DenialsKind, b)
}

// RegisterApplicationLogsBuilder registers a new Logger builder.
func (r *registry) RegisterApplicationLogsBuilder(b adapter.ApplicationLogsBuilder) {
	r.insert(aspect.ApplicationLogsKind, b)
}

// RegisterAccessLogsBuilder registers a new Logger builder.
func (r *registry) RegisterAccessLogsBuilder(b adapter.AccessLogsBuilder) {
	r.insert(aspect.AccessLogsKind, b)
}

// RegisterQuotasBuilder registers a new Quotas builder.
func (r *registry) RegisterQuotasBuilder(b adapter.QuotasBuilder) {
	r.insert(aspect.QuotasKind, b)
}

// RegisterMetricsBuilder registers a new Metrics builder.
func (r *registry) RegisterMetricsBuilder(b adapter.MetricsBuilder) {
	r.insert(aspect.MetricsKind, b)
}

func (r *registry) insert(k aspect.Kind, b adapter.Builder) {
	kind := k.String()
	ok := true
	if glog.V(1) {
		defer func() {
			if ok {
				glog.Infof("Registered %s / %s", kind, b.Name())
			}
		}()
	}

	bi := r.builders[b.Name()]
	if bi == nil {
		r.builders[b.Name()] = &builderInfo{b, []string{kind}}
		return
	}

	// panic only if 2 different builder objects are trying to identify by the
	// same Name.  2nd registration is ok so long as old and the new are same
	if bi.Builder != b {
		ok = false
		panic(fmt.Errorf("duplicate registration for '%s' : old = %v new = %v", b.Name(), bi.Builder, b))
	}

	// 2nd registration of the same adapter
	// check if kind is already registered
	for _, k := range bi.Kinds {
		if k == kind {
			return
		}
	}

	bi.Kinds = append(bi.Kinds, kind)
}
