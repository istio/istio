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

package v1alpha1

import (
	"encoding/json"

	"github.com/gogo/protobuf/jsonpb"
	github_com_golang_protobuf_jsonpb "github.com/golang/protobuf/jsonpb"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	intv              int64 = 0
	stringv           int64 = 1
	IntOrStringInt          = &intv
	IntOrStringString       = &stringv
)

// UnmarshalJSON implements the json.Unmarshaller interface.
func (this *IntOrString) UnmarshalJSON(value []byte) error {
	if value[0] == '"' {
		this.Type = IntOrStringString
		return json.Unmarshal(value, &this.StrVal)
	}
	this.Type = IntOrStringInt
	return json.Unmarshal(value, &this.IntVal)
}

func (this *IntOrString) MarshalJSONPB(_ *jsonpb.Marshaler) ([]byte, error) {
	return this.MarshalJSON()
}

func (this *IntOrString) MarshalJSON() ([]byte, error) {
	if this.IntVal != nil {
		return json.Marshal(this.IntVal)
	}
	return json.Marshal(this.StrVal)
}

func (this *IntOrString) UnmarshalJSONPB(_ *github_com_golang_protobuf_jsonpb.Unmarshaler, value []byte) error {
	return this.UnmarshalJSON(value)
}

func (this *IntOrString) ToKubernetes() intstr.IntOrString {
	if this.IntVal != nil {
		return intstr.FromInt(int(this.GetIntVal()))
	}
	return intstr.FromString(this.GetStrVal())
}
