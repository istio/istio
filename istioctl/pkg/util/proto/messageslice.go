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

package proto

import (
	"bytes"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
)

// MessageSlice allows us to marshal slices of protobuf messages like clusters/listeners/routes correctly
type MessageSlice []proto.Message

// MarshalJSON handles marshaling of slices of proto messages
func (m MessageSlice) MarshalJSON() ([]byte, error) {
	return marshalWith(m, jsonpb.Marshaler{})
}

// MarshalIndentedJSON marshals a slice of protos into human-readable JSON indented with spaces
func (m MessageSlice) MarshalIndentedJSON() ([]byte, error) {
	return marshalWith(m, jsonpb.Marshaler{Indent: "    "})
}

func marshalWith(m MessageSlice, marshaler jsonpb.Marshaler) ([]byte, error) {
	buffer := bytes.NewBufferString("[")
	sliceLength := len(m)
	for index, msg := range m {
		if err := marshaler.Marshal(buffer, msg); err != nil {
			return nil, err
		}
		if index < sliceLength-1 {
			buffer.WriteString(",")
		}
	}
	buffer.WriteString("]")
	return buffer.Bytes(), nil
}
