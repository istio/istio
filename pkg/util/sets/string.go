// Copyright 2018 Istio Authors
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

package sets

// Empty is an empty struct used to mark existence of elements in set.
type Empty struct{}

// String is a set of string for minimal memory consumption.
type String map[string]Empty

// NewString creates a string set based on a slice of strings.
func NewString(eles ...string) String {
	ss := String{}
	ss.Insert(eles...)
	return ss
}

// Insert inserts a slice of strings into the set.
func (s String) Insert(eles ...string) {
	for _, ele := range eles {
		s[ele] = Empty{}
	}
}

// Has returns true iff the set has the element.
func (s String) Has(ele string) bool {
	_, ok := s[ele]
	return ok
}
