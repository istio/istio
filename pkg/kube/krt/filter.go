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
	"istio.io/istio/pkg/util/sets"
)

type filter struct {
	keys sets.String

	// selectsNonEmpty is like selects, but it treats an empty selector as not matching
	selectsNonEmpty map[string]string
	selects         map[string]string
	labels          map[string]string
	generic         func(any) bool

	listFromIndex func() any
	indexMatches  func(any) bool
}

func (f filter) String() string {
	attrs := []string{}
	if !f.keys.IsEmpty() {
		attrs = append(attrs, "key="+f.keys.String())
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
		h.filter.keys = sets.New(keyFunc(name.Name, name.Namespace))
	}
}

func FilterKey(k string) FetchOption {
	return func(h *dependency) {
		h.filter.keys = sets.New(k)
	}
}

func FilterKeys(k ...string) FetchOption {
	return func(h *dependency) {
		h.filter.keys = sets.New(k...)
	}
}

// FilterSelects only includes objects that select this label. If the selector is empty, it is a match.
func FilterSelects(lbls map[string]string) FetchOption {
	return func(h *dependency) {
		h.filter.selects = lbls
	}
}

// FilterIndex selects only objects matching a key in an index.
func FilterIndex[I any, K comparable](idx *Index[I, K], k K) FetchOption {
	return func(h *dependency) {
		// Index is used to pre-filter on the List, and also to match in Matches. Provide type-erased methods for both
		h.filter.listFromIndex = func() any {
			return idx.Lookup(k)
		}
		h.filter.indexMatches = func(a any) bool {
			return idx.objectHasKey(a.(I), k)
		}
	}
}

// FilterSelectsNonEmpty only includes objects that select this label. If the selector is empty, it is not a match.
func FilterSelectsNonEmpty(lbls map[string]string) FetchOption {
	return func(h *dependency) {
		h.filter.selectsNonEmpty = lbls
	}
}

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

func (f filter) Matches(object any, forList bool) bool {
	// Check each of our defined filters to see if the object matches
	// This function is called very often and is important to keep fast
	// Cheaper checks should come earlier to avoid additional work and short circuit early

	// First, lookup directly by key. This is cheap
	// an empty set will match none
	if f.keys != nil && !f.keys.Contains(string(GetKey[any](object))) {
		if log.DebugEnabled() {
			log.Debugf("no match key: %q vs %q", f.keys, string(GetKey[any](object)))
		}
		return false
	}

	// Index is also cheap, and often used to filter namespaces out. Make sure we do this early
	// If we are listing, we already did this. Do not redundantly check.
	if !forList {
		if f.indexMatches != nil && !f.indexMatches(object) {
			if log.DebugEnabled() {
				log.Debugf("no match index")
			}
			return false
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
