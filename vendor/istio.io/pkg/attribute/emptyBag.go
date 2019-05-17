// Copyright 2017 Istio Authors
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

// emptyBag is an attribute bag that is always empty. It is primarily used as a backstop in a
// chain of bags
type emptyBag struct{}

var empty = &emptyBag{}
var emptySlice = []string{}

func (eb *emptyBag) Get(name string) (interface{}, bool) { return nil, false }
func (eb *emptyBag) Names() []string                     { return emptySlice }
func (eb *emptyBag) Contains(key string) bool            { return false }
func (eb *emptyBag) Done()                               {}
func (eb *emptyBag) String() string                      { return "" }
func (eb *emptyBag) ReferenceTracker() ReferenceTracker  { return nil }
