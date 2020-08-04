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

package label

import (
	"sort"
	"strings"
)

// Instance is a label instance.
type Instance string

// Set is a set of labels
type Set map[Instance]struct{}

// NewSet returns a new label set.
func NewSet(labels ...Instance) Set {
	s := make(map[Instance]struct{})
	for _, l := range labels {
		s[l] = struct{}{}
	}

	return s
}

// Add adds the given labels and returns a new, combined set
func (l Set) Add(labels ...Instance) Set {
	c := l.Clone()
	for _, label := range labels {
		c[label] = struct{}{}
	}

	return c
}

// Merge returns a set that is merging of l and s.
func (l Set) Merge(s Set) Set {
	c := l.Clone()
	for k, v := range s {
		c[k] = v
	}

	return c
}

// Clone this set of labels
func (l Set) Clone() Set {
	s := make(map[Instance]struct{})
	for k, v := range l {
		s[k] = v
	}

	return s
}

// All returns all labels in this set.
func (l Set) All() []Instance {
	r := make([]Instance, 0, len(l))
	for label := range l {
		r = append(r, label)
	}

	sort.Slice(r, func(i, j int) bool {
		return strings.Compare(string(r[i]), string(r[j])) < 0
	})
	return r
}

func (l Set) contains(label Instance) bool {
	_, found := l[label]
	return found
}

func (l Set) containsAny(other Set) bool {
	for l2 := range other {
		if l.contains(l2) {
			return true
		}
	}

	return false
}

func (l Set) containsAll(other Set) bool {
	for l2 := range other {
		if l.contains(l2) {
			continue
		}
		return false
	}

	return true
}
