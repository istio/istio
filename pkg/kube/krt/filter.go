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

package krt

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/smallset"
)

type filter struct {
	keys smallset.Set[string]

	// selectsNonEmpty is like selects, but it treats an empty selector as not matching
	selectsNonEmpty map[string]string
	selects         map[string]string
	labels          map[string]string
	generic         func(any) bool

	index *indexFilter
}

type indexFilter struct {
	list         func() any
	indexMatches func(any) bool
	extractKeys  objectKeyExtractor
	key          string
}

type objectKeyExtractor = func(o any) []string

func getKeyExtractor(o any) []string {
	return []string{GetKey(o)}
}

// reverseIndexKey
func (f *filter) reverseIndexKey() ([]string, indexedDependencyType, objectKeyExtractor, bool) {
	if f.keys.Len() > 0 {
		if f.index != nil {
			panic("cannot filter by index and key")
		}
		return f.keys.List(), getKeyType, getKeyExtractor, true
	}
	if f.index != nil {
		return []string{f.index.key}, indexType, f.index.extractKeys, true
	}
	return nil, unknownIndexType, nil, false
}

func (f *filter) String() string {
	attrs := []string{}
	if !f.keys.IsNil() {
		attrs = append(attrs, "keys="+f.keys.String())
	}
	if f.selectsNonEmpty != nil {
		attrs = append(attrs, fmt.Sprintf("selectsNonEmpty=%v", f.selectsNonEmpty))
	}
	if f.selects != nil {
		attrs = append(attrs, fmt.Sprintf("selects=%v", f.selects))
	}
	if f.labels != nil {
		attrs = append(attrs, fmt.Sprintf("labels=%v", f.labels))
	}
	if f.generic != nil {
		attrs = append(attrs, "generic")
	}
	res := strings.Join(attrs, ",")
	return fmt.Sprintf("{%s}", res)
}

// FilterObjectName selects a Kubernetes object by name.
func FilterObjectName(name types.NamespacedName) FetchOption {
	return func(h *dependency) {
		// Translate to a key lookup
		h.filter.keys = smallset.New(keyFunc(name.Name, name.Namespace))
	}
}

func FilterKey(k string) FetchOption {
	return func(h *dependency) {
		h.filter.keys = smallset.New(k)
	}
}

func FilterKeys(k ...string) FetchOption {
	return func(h *dependency) {
		h.filter.keys = smallset.New(k...)
	}
}

// FilterSelects only includes objects that select this label. If the selector is empty, it is a match.
func FilterSelects(lbls map[string]string) FetchOption {
	return func(h *dependency) {
		h.filter.selects = lbls
	}
}

// FilterIndex selects only objects matching a key in an index.
// NOTE: A single transformation function cannot call FilterIndex
// using multiple indexes for the same collection.
func FilterIndex[K comparable, I any](idx Index[K, I], k K) FetchOption {
	return func(h *dependency) {
		// Index is used to pre-filter on the List, and also to match in Matches. Provide type-erased methods for both
		h.filter.index = &indexFilter{
			list: func() any {
				return idx.Lookup(k)
			},
			indexMatches: func(a any) bool {
				return idx.objectHasKey(a.(I), k)
			},
			extractKeys: func(o any) []string {
				return slices.Map(idx.extractKeys(o.(I)), func(e K) string {
					return toString(e)
				})
			},
			key: toString(k),
		}
	}
}

// FilterSelectsNonEmpty only includes objects that select this label. If the selector is empty, it is NOT a match.
func FilterSelectsNonEmpty(lbls map[string]string) FetchOption {
	return func(h *dependency) {
		// Need to distinguish empty vs unset. A user may pass in 'lbls' as nil, this doesn't mean they do not want it to filter at all.
		if lbls == nil {
			lbls = make(map[string]string)
		}
		h.filter.selectsNonEmpty = lbls
	}
}

// FilterLabel only includes objects that match the provided labels. If the selector is empty, it IS a match.
func FilterLabel(lbls map[string]string) FetchOption {
	return func(h *dependency) {
		h.filter.labels = lbls
	}
}

func FilterGeneric(f func(any) bool) FetchOption {
	return func(h *dependency) {
		h.filter.generic = f
	}
}

func (f *filter) Matches(object any, forList bool) bool {
	// Check each of our defined filters to see if the object matches
	// This function is called very often and is important to keep fast
	// Cheaper checks should come earlier to avoid additional work and short circuit early

	// If we are listing, we already did this. Do not redundantly check.
	if !forList {
		// First, lookup directly by key. This is cheap
		// an empty set will match none
		if !f.keys.IsNil() && !f.keys.Contains(GetKey[any](object)) {
			if log.DebugEnabled() {
				log.Debugf("no match key: %q vs %q", f.keys, GetKey[any](object))
			}
			return false
		}
		// Index is also cheap, and often used to filter namespaces out. Make sure we do this early
		if f.index != nil {
			if !f.index.indexMatches(object) {
				if log.DebugEnabled() {
					log.Debugf("no match index")
				}
				return false
			}
		}
	}

	// Rest is expensive
	if f.selects != nil && !labels.Instance(getLabelSelector(object)).SubsetOf(f.selects) {
		if log.DebugEnabled() {
			log.Debugf("no match selects: %q vs %q", f.selects, getLabelSelector(object))
		}
		return false
	}
	if f.selectsNonEmpty != nil && !labels.Instance(getLabelSelector(object)).Match(f.selectsNonEmpty) {
		if log.DebugEnabled() {
			log.Debugf("no match selectsNonEmpty: %q vs %q", f.selectsNonEmpty, getLabelSelector(object))
		}
		return false
	}
	if f.labels != nil && !labels.Instance(f.labels).SubsetOf(getLabels(object)) {
		if log.DebugEnabled() {
			log.Debugf("no match labels: %q vs %q", f.labels, getLabels(object))
		}
		return false
	}
	if f.generic != nil && !f.generic(object) {
		if log.DebugEnabled() {
			log.Debugf("no match generic")
		}
		return false
	}
	return true
}
