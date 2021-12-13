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
func (i *IntOrString) UnmarshalJSON(value []byte) error {
	if value[0] == '"' {
		i.Type = IntOrStringString
		return json.Unmarshal(value, &i.StrVal)
	}
	i.Type = IntOrStringInt
	return json.Unmarshal(value, &i.IntVal)
}

func (i *IntOrString) MarshalJSONPB(_ *jsonpb.Marshaler) ([]byte, error) {
	return i.MarshalJSON()
}

func (i *IntOrString) MarshalJSON() ([]byte, error) {
	if i.IntVal != nil {
		return json.Marshal(i.IntVal)
	}
	return json.Marshal(i.StrVal)
}

func (i *IntOrString) UnmarshalJSONPB(_ *github_com_golang_protobuf_jsonpb.Unmarshaler, value []byte) error {
	return i.UnmarshalJSON(value)
}

func (i *IntOrString) ToKubernetes() intstr.IntOrString {
	if i.IntVal != nil {
		return intstr.FromInt(int(i.GetIntVal()))
	}
	return intstr.FromString(i.GetStrVal())
}
