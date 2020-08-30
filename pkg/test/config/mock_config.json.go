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

package config

import (
	bytes "bytes"
	fmt "fmt"
	math "math"

	github_com_gogo_protobuf_jsonpb "github.com/gogo/protobuf/jsonpb"
	proto "github.com/gogo/protobuf/proto"

	_ "istio.io/gogo-genproto/googleapis/google/api"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// MarshalJSON is a custom marshaler for MockConfig
func (this *MockConfig) MarshalJSON() ([]byte, error) {
	str, err := MockConfigMarshaler.MarshalToString(this)
	return []byte(str), err
}

// UnmarshalJSON is a custom unmarshaler for MockConfig
func (this *MockConfig) UnmarshalJSON(b []byte) error {
	return MockConfigUnmarshaler.Unmarshal(bytes.NewReader(b), this)
}

var (
	MockConfigMarshaler   = &github_com_gogo_protobuf_jsonpb.Marshaler{}
	MockConfigUnmarshaler = &github_com_gogo_protobuf_jsonpb.Unmarshaler{}
)
