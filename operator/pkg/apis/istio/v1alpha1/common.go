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
	"istio.io/api/operator/v1alpha1"

	"github.com/gogo/protobuf/jsonpb"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	globalKey         = "global"
	istioNamespaceKey = "istioNamespace"
)

// Namespace returns the namespace of the containing CR.
func Namespace(iops *v1alpha1.IstioOperatorSpec) string {
	if iops.Namespace != "" {
		return iops.Namespace
	}
	if iops.Values == nil {
		return ""
	}
	if iops.Values[globalKey] == nil {
		return ""
	}
	v := iops.Values[globalKey].(map[string]interface{})
	n := v[istioNamespaceKey]
	if n == nil {
		return ""
	}
	return n.(string)
}

// SetNamespace returns the namespace of the containing CR.
func SetNamespace(iops *v1alpha1.IstioOperatorSpec, namespace string) {
	if namespace != "" {
		iops.Namespace = namespace
	}
	if iops.Values == nil {
		iops.Values = make(map[string]interface{})
	}
	if iops.Values[globalKey] == nil {
		iops.Values[globalKey] = make(map[string]interface{})
	}
	v := iops.Values[globalKey].(map[string]interface{})
	v[istioNamespaceKey] = namespace
}

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
