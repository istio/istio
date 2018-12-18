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

package cel

import (
	"errors"
	"reflect"
	"time"

	"github.com/golang/protobuf/ptypes"
	dpb "github.com/golang/protobuf/ptypes/duration"
	tpb "github.com/golang/protobuf/ptypes/timestamp"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/lang"
)

func convertType(typ v1beta1.ValueType) *exprpb.Type {
	switch typ {
	case v1beta1.STRING:
		return &exprpb.Type{TypeKind: &exprpb.Type_Primitive{Primitive: exprpb.Type_STRING}}
	case v1beta1.INT64:
		return &exprpb.Type{TypeKind: &exprpb.Type_Primitive{Primitive: exprpb.Type_INT64}}
	case v1beta1.DOUBLE:
		return &exprpb.Type{TypeKind: &exprpb.Type_Primitive{Primitive: exprpb.Type_DOUBLE}}
	case v1beta1.BOOL:
		return &exprpb.Type{TypeKind: &exprpb.Type_Primitive{Primitive: exprpb.Type_BOOL}}
	case v1beta1.TIMESTAMP:
		return &exprpb.Type{TypeKind: &exprpb.Type_WellKnown{WellKnown: exprpb.Type_TIMESTAMP}}
	case v1beta1.DURATION:
		return &exprpb.Type{TypeKind: &exprpb.Type_WellKnown{WellKnown: exprpb.Type_DURATION}}
	case v1beta1.STRING_MAP:
		return stringMapType
	case v1beta1.IP_ADDRESS:
		return ipAddressType
	case v1beta1.EMAIL_ADDRESS:
		return emailAddressType
	case v1beta1.URI:
		return uriType
	case v1beta1.DNS_NAME:
		return dnsType
	}
	return &exprpb.Type{TypeKind: &exprpb.Type_Dyn{}}
}

func convertValue(typ v1beta1.ValueType, value interface{}) ref.Value {
	switch typ {
	case v1beta1.STRING, v1beta1.INT64, v1beta1.DOUBLE, v1beta1.BOOL:
		return types.NativeToValue(value)
	case v1beta1.TIMESTAMP:
		t := value.(time.Time)
		tproto, err := ptypes.TimestampProto(t)
		if err != nil {
			return types.NewErr("incorrect timestamp: %v", err)
		}
		return types.Timestamp{Timestamp: tproto}
	case v1beta1.DURATION:
		d := value.(time.Duration)
		return types.Duration{Duration: ptypes.DurationProto(d)}
	case v1beta1.STRING_MAP:
		sm := value.(attribute.StringMap)
		return stringMapValue{value: sm}
	case v1beta1.IP_ADDRESS:
		return wrapperValue{typ: typ, bytes: value.([]byte)}
	case v1beta1.EMAIL_ADDRESS:
	case v1beta1.URI:
	case v1beta1.DNS_NAME:
		return wrapperValue{typ: typ, s: value.(string)}
	}
	return types.NewErr("cannot convert value %#v of type %q", value, typ)
}

func defaultValue(typ v1beta1.ValueType) ref.Value {
	switch typ {
	case v1beta1.STRING:
		return types.String("")
	case v1beta1.INT64:
		return types.Int(0)
	case v1beta1.DOUBLE:
		return types.Double(0)
	case v1beta1.BOOL:
		return types.Bool(false)
	case v1beta1.TIMESTAMP:
		return types.Timestamp{Timestamp: &tpb.Timestamp{}}
	case v1beta1.DURATION:
		return types.Duration{Duration: &dpb.Duration{}}
	case v1beta1.STRING_MAP:
		return emptyStringMap
	case v1beta1.IP_ADDRESS, v1beta1.EMAIL_ADDRESS, v1beta1.URI, v1beta1.DNS_NAME:
		return wrapperValue{typ: typ}
	}
	return types.NewErr("cannot provide defaults for %q", typ)
}

var (
	stringMapType = &exprpb.Type{TypeKind: &exprpb.Type_MapType_{MapType: &exprpb.Type_MapType{
		KeyType:   &exprpb.Type{TypeKind: &exprpb.Type_Primitive{Primitive: exprpb.Type_STRING}},
		ValueType: &exprpb.Type{TypeKind: &exprpb.Type_Primitive{Primitive: exprpb.Type_STRING}},
	}}}
	emptyStringMap = stringMapValue{value: attribute.NewStringMap("")}

	ipAddressType    = decls.NewObjectType("istio.policy.v1beta1.IPAddress")
	emailAddressType = decls.NewObjectType("istio.policy.v1beta1.EmailAddress")
	uriType          = decls.NewObjectType("istio.policy.v1beta1.Uri")
	dnsType          = decls.NewObjectType("istio.policy.v1beta1.DNSName")

	// domain specific types do not implement any of type traits for now
	wrapperType = types.NewTypeValue("wrapper")
)

type stringMapValue struct {
	value attribute.StringMap
}

func (v stringMapValue) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	return nil, errors.New("cannot convert stringmap to native types")
}
func (v stringMapValue) ConvertToType(typeValue ref.Type) ref.Value {
	return types.NewErr("cannot convert stringmap to CEL types")
}
func (v stringMapValue) Equal(other ref.Value) ref.Value {
	return types.NewErr("stringmap does not support equality")
}
func (v stringMapValue) Type() ref.Type {
	return types.MapType
}
func (v stringMapValue) Value() interface{} {
	return v
}
func (v stringMapValue) Get(index ref.Value) ref.Value {
	if index.Type() != types.StringType {
		return types.NewErr("index should be a string")
	}

	field := index.Value().(string)
	value, found := v.value.Get(field)
	if found {
		return types.String(value)
	}
	return types.NewErr("no such key: '%s'", field)
}
func (v stringMapValue) Contains(index ref.Value) ref.Value {
	if index.Type() != types.StringType {
		return types.NewErr("index should be a string")
	}

	field := index.Value().(string)
	_, found := v.value.Get(field)
	return types.Bool(found)
}
func (v stringMapValue) Size() ref.Value {
	return types.NewErr("size not implemented on stringmaps")
}

type wrapperValue struct {
	typ   v1beta1.ValueType
	bytes []byte
	s     string
}

func (v wrapperValue) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	return nil, errors.New("cannot convert wrapper value to native types")
}
func (v wrapperValue) ConvertToType(typeValue ref.Type) ref.Value {
	return types.NewErr("cannot convert wrapper value  to CEL types")
}
func (v wrapperValue) Equal(other ref.Value) ref.Value {
	if other.Type() != wrapperType {
		return types.NewErr("cannot compare types")
	}
	w, ok := other.(wrapperValue)
	if !ok {
		return types.NewErr("cannot compare types")
	}
	if v.typ != w.typ {
		return types.NewErr("cannot compare %s and %s", v.typ, w.typ)
	}
	var out bool
	var err error
	switch v.typ {
	case v1beta1.IP_ADDRESS:
		out = lang.ExternIPEqual(v.bytes, w.bytes)
	case v1beta1.DNS_NAME:
		out, err = lang.ExternDNSNameEqual(v.s, w.s)
	case v1beta1.EMAIL_ADDRESS:
		out, err = lang.ExternEmailEqual(v.s, w.s)
	case v1beta1.URI:
		out, err = lang.ExternURIEqual(v.s, w.s)
	}
	if err != nil {
		return types.NewErr(err.Error())
	}
	return types.Bool(out)
}
func (v wrapperValue) Type() ref.Type {
	return wrapperType
}
func (v wrapperValue) Value() interface{} {
	switch v.typ {
	case v1beta1.IP_ADDRESS:
		return v.bytes
	default:
		return v.s
	}
}
