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

package v1alpha2

// TODO: create remaining enum types.

import (
	"encoding/json"

	"github.com/gogo/protobuf/jsonpb"
	protobuf "github.com/gogo/protobuf/types"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// define new type from k8s intstr to marshal/unmarshal jsonpb
type IntOrStringForPB struct {
	intstr.IntOrString
}

// MarshalJSONPB implements the jsonpb.JSONPBMarshaler interface.
func (intstrpb *IntOrStringForPB) MarshalJSONPB(_ *jsonpb.Marshaler) ([]byte, error) {
	return intstrpb.MarshalJSON()
}

// UnmarshalJSONPB implements the jsonpb.JSONPBUnmarshaler interface.
func (intstrpb *IntOrStringForPB) UnmarshalJSONPB(_ *jsonpb.Unmarshaler, value []byte) error {
	return intstrpb.UnmarshalJSON(value)
}

// FromInt creates an IntOrStringForPB object with an int32 value.
func FromInt(val int) IntOrStringForPB {
	return IntOrStringForPB{intstr.FromInt(val)}
}

// FromString creates an IntOrStringForPB object with a string value.
func FromString(val string) IntOrStringForPB {
	return IntOrStringForPB{intstr.FromString(val)}
}

// IstioControlPlaneList contains a list of IstioControlPlane
type IstioControlPlaneList struct {
	v1.TypeMeta `json:",inline"`
	v1.ListMeta `json:"metadata,omitempty"`
	Items       []IstioControlPlane `json:"items"`
}

// define new type from protobuf.BoolValue to marshal/unmarshal jsonpb
type BoolValueForPB struct {
	protobuf.BoolValue
}

// MarshalJSON implements the json.JSONMarshaler interface.
func (boolvaluepb *BoolValueForPB) MarshalJSON() ([]byte, error) {
	return json.Marshal(boolvaluepb.GetValue())
}

// UnmarshalJSON implements the json.JSONUnmarshaler interface.
func (boolvaluepb *BoolValueForPB) UnmarshalJSON(value []byte) error {
	return json.Unmarshal(value, &(boolvaluepb.Value))
}

// MarshalJSONPB implements the jsonpb.JSONPBMarshaler interface.
func (boolvaluepb *BoolValueForPB) MarshalJSONPB(_ *jsonpb.Marshaler) ([]byte, error) {
	return boolvaluepb.MarshalJSON()
}

// UnmarshalJSONPB implements the jsonpb.JSONPBUnmarshaler interface.
func (boolvaluepb *BoolValueForPB) UnmarshalJSONPB(_ *jsonpb.Unmarshaler, value []byte) error {
	return boolvaluepb.UnmarshalJSON(value)
}
