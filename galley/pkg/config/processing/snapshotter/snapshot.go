// Copyright Istio Authors
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

package snapshotter

import (
	"fmt"
	"strconv"
	"strings"

	mcp "istio.io/api/mcp/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	coll "istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/mcp/snapshot"
)

// Snapshot is an implementation of MCP's snapshot.Snapshot interface. It also exposes additional query methods
// for analysis purposes.
type Snapshot struct {
	set *coll.Set
}

var _ snapshot.Snapshot = &Snapshot{}

// Resources implements snapshotImpl.Snapshot
func (s *Snapshot) Resources(col string) []*mcp.Resource {
	c := s.set.Collection(collection.NewName(col))

	if c == nil {
		return nil
	}

	result := make([]*mcp.Resource, 0, c.Size())

	s.set.Collection(collection.NewName(col)).ForEach(func(r *resource.Instance) bool {
		// TODO: We should add (LRU based) caching of serialized content here.
		rs, err := resource.Serialize(r)
		if err != nil {
			scope.Processing.Errorf("Unable to serialize resource.Instance: %v", err)
		} else {
			result = append(result, rs)
		}
		return true
	})

	return result
}

// Version implements snapshotImpl.Snapshot
func (s *Snapshot) Version(col string) string {
	coll := s.set.Collection(collection.NewName(col))
	if coll == nil {
		return ""
	}
	g := coll.Generation()
	return col + "/" + strconv.FormatInt(g, 10)
}

// Collections implements snapshotImpl.Snapshot
func (s *Snapshot) Collections() []string {
	names := s.set.Names()
	result := make([]string, 0, len(names))

	for _, name := range names {
		result = append(result, name.String())
	}

	return result
}

// Find the resource with the given name and collection.
func (s *Snapshot) Find(cpl collection.Name, name resource.FullName) *resource.Instance {
	c := s.set.Collection(cpl)
	if c == nil {
		return nil
	}
	return c.Get(name)
}

// ForEach iterates all resources in a given collection.
func (s *Snapshot) ForEach(col collection.Name, fn analysis.IteratorFn) {
	c := s.set.Collection(col)
	if c == nil {
		return
	}
	c.ForEach(fn)
}

// String implements io.Stringer
func (s *Snapshot) String() string {
	var b strings.Builder

	for i, n := range s.set.Names() {
		b.WriteString(fmt.Sprintf("[%d] %s (@%s)\n", i, n.String(), s.Version(n.String())))
		for j, e := range s.Resources(n.String()) {
			b.WriteString(fmt.Sprintf("  [%d] %s\n", j, e.Metadata.Name))
		}
	}

	return b.String()
}
