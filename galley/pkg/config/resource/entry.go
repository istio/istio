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

// Package resource contains core abstract types for representing configuration resources.
package resource

import (
	"github.com/gogo/protobuf/proto"
)

// Entry is the abstract representation of a versioned config resource in Istio.
type Entry struct {
	Metadata Metadata
	Item     proto.Message
	Origin   Origin
}

// IsEmpty returns true if the resource Entry.Item is nil.
func (r *Entry) IsEmpty() bool {
	return r.Item == nil
}

// Clone returns a deep-copy of this entry. Warning, this is expensive!
func (r *Entry) Clone() *Entry {
	result := &Entry{}
	if r.Item != nil {
		result.Item = proto.Clone(r.Item)
	}
	result.Metadata = r.Metadata.Clone()
	return result
}
