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

// Package resource contains core abstract types for representing configuration resources.
package resource

import (
	"github.com/gogo/protobuf/proto"
)

// Instance is the abstract representation of a versioned config resource in Istio.
type Instance struct {
	Metadata Metadata
	Message  proto.Message
	Origin   Origin
}

// IsEmpty returns true if the resource Instance.Message is nil.
func (r *Instance) IsEmpty() bool {
	return r.Message == nil
}

// Clone returns a deep-copy of this entry. Warning, this is expensive!
func (r *Instance) Clone() *Instance {
	result := &Instance{}
	if r.Message != nil {
		result.Message = proto.Clone(r.Message)
	}
	result.Metadata = r.Metadata.Clone()
	return result
}
