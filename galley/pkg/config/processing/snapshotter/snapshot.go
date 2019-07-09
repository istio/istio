// Copyright 2019 Istio Authors
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
	"strconv"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/pkg/mcp/snapshot"
)

type snapshotImpl struct {
	set *collection.Set
}

var _ snapshot.Snapshot = &snapshotImpl{}

// Resources implements snapshotImpl.Snapshot
func (s *snapshotImpl) Resources(col string) []*mcp.Resource {
	c := s.set.Collection(collection.NewName(col))

	result := make([]*mcp.Resource, 0, c.Size())

	s.set.Collection(collection.NewName(col)).ForEach(func(e *resource.Entry) {
		// TODO: We should add (LRU based) caching of serialized content here.
		r, err := resource.Serialize(e)
		if err != nil {
			scope.Errorf("Unable to serialize resource.Entry: %v", err)
		} else {
			result = append(result, r)
		}
	})

	return result
}

// Version implements snapshotImpl.Snapshot
func (s *snapshotImpl) Version(col string) string {
	coll := s.set.Collection(collection.NewName(col))
	if coll == nil {
		return ""
	}
	g := coll.Generation()
	return col + "/" + strconv.FormatInt(g, 10)
}
