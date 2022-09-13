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

package proto

import (
	"bytes"

	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/util/protomarshal"
)

// MessageSlice allows us to marshal slices of protobuf messages like clusters/listeners/routes/endpoints correctly
type MessageSlice []proto.Message

// MarshalJSON handles marshaling of slices of proto messages
func (pSlice MessageSlice) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString("[")
	sliceLength := len(pSlice)
	for index, msg := range pSlice {
		b, err := protomarshal.Marshal(msg)
		if err != nil {
			return nil, err
		}
		buffer.Write(b)
		if index < sliceLength-1 {
			buffer.WriteString(",")
		}
	}
	buffer.WriteString("]")
	return buffer.Bytes(), nil
}
