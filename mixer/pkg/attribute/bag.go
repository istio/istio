// Copyright 2016 Istio Authors
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

package attribute

import "fmt"

// Bag is a generic mechanism to access a set of attributes.
type Bag interface {
	fmt.Stringer

	// Get returns an attribute value.
	Get(name string) (value interface{}, found bool)

	// Names returns the names of all the attributes known to this bag.
	Names() []string

	// Contains returns true if this bag contains the specified key
	Contains(key string) bool

	// Done indicates the bag can be reclaimed.
	Done()
}
