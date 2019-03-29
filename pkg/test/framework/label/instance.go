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

package label

// Instance is a label instance.
type Instance string

// Set is a set of labels
type Set []Instance

// NewSet returns a new label set.
func NewSet(labels ...Instance) Set {
	return Set(labels)
}

// Join returns a set that is concatenation of l and s.
func (l Set) Join(s Set) Set {
	r := make([]Instance, 0, len(s)+len(l))
	r = append(r, l...)
	r = append(r, s...)
	return r
}

func (l Set) contains(label Instance) bool {
	for _, l1 := range l {
		if l1 == label {
			return true
		}
	}
	return false
}

func (l Set) containsAny(other Set) bool {
	for _, l2 := range other {
		if l.contains(l2) {
			return true
		}
	}

	return false
}

func (l Set) containsAll(other Set) bool {
	for _, l2 := range other {
		if l.contains(l2) {
			continue
		}
		return false
	}

	return true
}
