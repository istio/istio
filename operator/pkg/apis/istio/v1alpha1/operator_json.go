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

// Configuration affecting Istio control plane installation version and shape.

package v1alpha1

import (
	bytes "bytes"
	"github.com/golang/protobuf/jsonpb" // nolint: depguard
)

// MarshalJSON is a custom marshaler for IstioOperatorSpec
func (in *IstioOperatorSpec) MarshalJSON() ([]byte, error) {
	str, err := OperatorMarshaler.MarshalToString(in)
	return []byte(str), err
}

// UnmarshalJSON is a custom unmarshaler for IstioOperatorSpec
func (in *IstioOperatorSpec) UnmarshalJSON(b []byte) error {
	return OperatorUnmarshaler.Unmarshal(bytes.NewReader(b), in)
}

var (
	OperatorMarshaler   = &jsonpb.Marshaler{}
	OperatorUnmarshaler = &jsonpb.Unmarshaler{}
)
