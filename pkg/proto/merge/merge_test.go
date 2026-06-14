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

package merge

import (
	"testing"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"google.golang.org/protobuf/types/known/durationpb"

	"istio.io/istio/pkg/test/util/assert"
)

func TestMerge(t *testing.T) {
	src := &durationpb.Duration{Seconds: 123, Nanos: 456}
	dst := &durationpb.Duration{Seconds: 789, Nanos: 999}

	srcListener := &listener.Listener{ListenerFiltersTimeout: src}
	dstListener := &listener.Listener{ListenerFiltersTimeout: dst}

	Merge(dstListener, srcListener)

	assert.Equal(t, dstListener.ListenerFiltersTimeout, src)

	// dst duration not changed after merge
	assert.Equal(t, dst, &durationpb.Duration{Seconds: 789, Nanos: 999})
}

func filterNames(filters []*listener.ListenerFilter) []string {
	names := make([]string, 0, len(filters))
	for _, f := range filters {
		names = append(names, f.GetName())
	}
	return names
}

// TestMerge_AppendsList verifies the default Merge semantics: repeated fields are appended.
func TestMerge_AppendsList(t *testing.T) {
	dst := &listener.Listener{ListenerFilters: []*listener.ListenerFilter{{Name: "a"}, {Name: "b"}}}
	src := &listener.Listener{ListenerFilters: []*listener.ListenerFilter{{Name: "c"}}}

	Merge(dst, src)

	assert.Equal(t, filterNames(dst.ListenerFilters), []string{"a", "b", "c"})
}

// TestMergeWithReplaceList verifies that lists in src fully replace lists in dst.
func TestMergeWithReplaceList(t *testing.T) {
	dst := &listener.Listener{
		Name:            "keep-me",
		ListenerFilters: []*listener.ListenerFilter{{Name: "a"}, {Name: "b"}},
	}
	src := &listener.Listener{ListenerFilters: []*listener.ListenerFilter{{Name: "c"}}}

	MergeWithReplaceList(dst, src)

	// The list is replaced, not appended.
	assert.Equal(t, filterNames(dst.ListenerFilters), []string{"c"})
	// Scalar fields not set by src are left untouched (sparse merge).
	assert.Equal(t, dst.Name, "keep-me")
}

// TestMergeWithReplaceList_EmptyListNotCleared verifies that a list absent from src
// leaves the dst list untouched (src.Range only visits set fields).
func TestMergeWithReplaceList_EmptyListNotCleared(t *testing.T) {
	dst := &listener.Listener{ListenerFilters: []*listener.ListenerFilter{{Name: "a"}}}
	src := &listener.Listener{Name: "set-name"}

	MergeWithReplaceList(dst, src)

	assert.Equal(t, filterNames(dst.ListenerFilters), []string{"a"})
	assert.Equal(t, dst.Name, "set-name")
}

// TestMergeWithReplaceList_Nested verifies replace semantics apply recursively into
// nested messages.
func TestMergeWithReplaceList_Nested(t *testing.T) {
	dst := &listener.Listener{
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{{Name: "f1"}, {Name: "f2"}},
		}},
	}
	src := &listener.Listener{
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{{Name: "f3"}},
		}},
	}

	MergeWithReplaceList(dst, src)

	// Top-level FilterChains list is replaced wholesale.
	assert.Equal(t, len(dst.FilterChains), 1)
	assert.Equal(t, dst.FilterChains[0].Filters[0].GetName(), "f3")
	assert.Equal(t, len(dst.FilterChains[0].Filters), 1)
}
