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

package resource

import "fmt"

// Resource of a resource.
type Resource interface {
	// ID used for debugging the resource instance.
	ID() ID
}

// ID for the resource instance. This is allocated by the framework and passed here.
type ID interface {
	fmt.Stringer
}

var _ ID = FakeID("")

// FakeID used for testing.
type FakeID string

func (id FakeID) String() string {
	return string(id)
}

var _ Resource = &FakeResource{}

// FakeResource used for testing.
type FakeResource struct {
	IDValue    string
	OtherValue string
}

func (f *FakeResource) ID() ID {
	return FakeID(f.IDValue)
}

// GetOtherValue is an additional method used to distinguish this resource API from others.
func (f *FakeResource) GetOtherValue() string {
	return f.OtherValue
}
