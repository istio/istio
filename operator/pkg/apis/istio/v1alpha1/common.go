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
	"math"

	"github.com/gogo/protobuf/types"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/pkg/util/gogoprotomarshal"
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
	v := AsMap(iops.Values)
	if v[globalKey] == nil {
		return ""
	}
	vg := v[globalKey].(map[string]interface{})
	n := vg[istioNamespaceKey]
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
	// TODO implement
}

func MustNewStruct(m map[string]interface{}) *types.Struct {
	r, err := NewStruct(m)
	if err != nil {
		panic(err.Error())
	}
	return r
}

func NewStruct(m map[string]interface{}) (*types.Struct, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	s := &types.Struct{}
	if err := gogoprotomarshal.ApplyJSON(string(b), s); err != nil {
		return nil, err
	}
	return s, nil
}

func AsMap(x *types.Struct) map[string]interface{} {
	vs := make(map[string]interface{})
	for k, v := range x.GetFields() {
		vs[k] = AsInterface(v)
	}
	return vs
}

func asSlice(x *types.ListValue) []interface{} {
	vs := make([]interface{}, len(x.GetValues()))
	for i, v := range x.GetValues() {
		vs[i] = AsInterface(v)
	}
	return vs
}

func AsInterface(x *types.Value) interface{} {
	switch v := x.GetKind().(type) {
	case *types.Value_NumberValue:
		if v != nil {
			switch {
			case math.IsNaN(v.NumberValue):
				return "NaN"
			case math.IsInf(v.NumberValue, +1):
				return "Infinity"
			case math.IsInf(v.NumberValue, -1):
				return "-Infinity"
			default:
				return v.NumberValue
			}
		}
	case *types.Value_StringValue:
		if v != nil {
			return v.StringValue
		}
	case *types.Value_BoolValue:
		if v != nil {
			return v.BoolValue
		}
	case *types.Value_StructValue:
		if v != nil {
			return AsMap(v.StructValue)
		}
	case *types.Value_ListValue:
		if v != nil {
			return asSlice(v.ListValue)
		}
	}
	return nil
}
