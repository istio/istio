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

package collection

import "regexp"

// Name of a collection.
type Name string

var validNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_\.]*(/[a-zA-Z0-9_][a-zA-Z0-9_\.]*)*$`)

// NewName returns a strongly typed collection. Panics if the name is not valid.
func NewName(n string) Name {
	if !IsValidName(n) {
		panic("collection.NewName: invalid collection name: " + n)
	}
	return Name(n)
}

// String interface method implementation.
func (n Name) String() string {
	return string(n)
}

// IsValidName returns true if the given collection is a valid name.
func IsValidName(name string) bool {
	return validNameRegex.Match([]byte(name))
}
