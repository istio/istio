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

package cel

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/golang/protobuf/ptypes"
	dpb "github.com/golang/protobuf/ptypes/duration"
	tpb "github.com/golang/protobuf/ptypes/timestamp"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang"
	"istio.io/pkg/attribute"
)

func convertType(typ v1beta1.ValueType) *exprpb.Type {
	switch typ {
	case v1beta1.STRING:
		return decls.String
	case v1beta1.INT64:
		return decls.Int
	case v1beta1.DOUBLE:
		return decls.Double
	case v1beta1.BOOL:
		return decls.Bool
	case v1beta1.TIMESTAMP:
		return decls.Timestamp
	case v1beta1.DURATION:
		return decls.Duration
	case v1beta1.STRING_MAP:
		return stringMapType
	case v1beta1.IP_ADDRESS:
		return decls.NewObjectType(ipAddressType)
	case v1beta1.EMAIL_ADDRESS:
		return decls.NewObjectType(emailAddressType)
	case v1beta1.URI:
		return decls.NewObjectType(uriType)
	case v1beta1.DNS_NAME:
		return decls.NewObjectType(dnsType)
	}
	return &exprpb.Type{TypeKind: &exprpb.Type_Dyn{}}
}

func recoverType(typ *exprpb.Type) v1beta1.ValueType {
	if typ == nil {
		return v1beta1.VALUE_TYPE_UNSPECIFIED
	}
	switch t := typ.TypeKind.(type) {
	case *exprpb.Type_Primitive:
		switch t.Primitive {
		case exprpb.Type_STRING:
			return v1beta1.STRING
		case exprpb.Type_INT64:
			return v1beta1.INT64
		case exprpb.Type_DOUBLE:
			return v1beta1.DOUBLE
		case exprpb.Type_BOOL:
			return v1beta1.BOOL
		}

	case *exprpb.Type_WellKnown:
		switch t.WellKnown {
		case exprpb.Type_TIMESTAMP:
			return v1beta1.TIMESTAMP
		case exprpb.Type_DURATION:
			return v1beta1.DURATION
		}

	case *exprpb.Type_MessageType:
		switch t.MessageType {
		case ipAddressType:
			return v1beta1.IP_ADDRESS
		case emailAddressType:
			return v1beta1.EMAIL_ADDRESS
		case uriType:
			return v1beta1.URI
		case dnsType:
			return v1beta1.DNS_NAME
		}

	case *exprpb.Type_MapType_:
		if reflect.DeepEqual(t.MapType.KeyType, decls.String) &&
			reflect.DeepEqual(t.MapType.ValueType, decls.String) {
			return v1beta1.STRING_MAP
		}

		// remaining maps are not yet supported
	}
	return v1beta1.VALUE_TYPE_UNSPECIFIED
}

func convertValue(typ v1beta1.ValueType, value interface{}) ref.Val {
	switch typ {
	case v1beta1.STRING, v1beta1.INT64, v1beta1.DOUBLE, v1beta1.BOOL:
		return types.DefaultTypeAdapter.NativeToValue(value)
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
	case v1beta1.EMAIL_ADDRESS, v1beta1.URI, v1beta1.DNS_NAME:
		return wrapperValue{typ: typ, s: value.(string)}
	}
	return types.NewErr("cannot convert value %#v of type %q", value, typ)
}

func recoverValue(value ref.Val) (interface{}, error) {
	switch value.Type() {
	case types.ErrType:
		if err, ok := value.Value().(error); ok {
			return nil, err
		}
		return nil, errors.New("unrecoverable error value")
	case types.StringType, types.IntType, types.DoubleType, types.BoolType:
		return value.Value(), nil
	case types.TimestampType:
		t := value.Value().(*tpb.Timestamp)
		return ptypes.Timestamp(t)
	case types.DurationType:
		d := value.Value().(*dpb.Duration)
		return ptypes.Duration(d)
	case types.MapType:
		size := value.(traits.Sizer).Size()
		if size.Type() == types.IntType && size.Value().(int64) == 0 {
			return emptyStringMap.value, nil
		}
		return value.Value(), nil
	case wrapperType:
		return value.Value(), nil
	case types.ListType:
		size := value.(traits.Sizer).Size()
		if size.Type() == types.IntType && size.Value().(int64) == 0 {
			return []string{}, nil
		}
		return value.Value(), nil
	}
	return nil, fmt.Errorf("failed to recover of type %s", value.Type())
}

var defaultValues = map[v1beta1.ValueType]ref.Val{
	v1beta1.STRING:        types.String(""),
	v1beta1.INT64:         types.Int(0),
	v1beta1.DOUBLE:        types.Double(0),
	v1beta1.BOOL:          types.Bool(false),
	v1beta1.TIMESTAMP:     types.Timestamp{Timestamp: &tpb.Timestamp{}},
	v1beta1.DURATION:      types.Duration{Duration: &dpb.Duration{}},
	v1beta1.STRING_MAP:    emptyStringMap,
	v1beta1.IP_ADDRESS:    wrapperValue{typ: v1beta1.IP_ADDRESS, bytes: []byte{}},
	v1beta1.EMAIL_ADDRESS: wrapperValue{typ: v1beta1.EMAIL_ADDRESS, s: ""},
	v1beta1.URI:           wrapperValue{typ: v1beta1.URI, s: ""},
	v1beta1.DNS_NAME:      wrapperValue{typ: v1beta1.DNS_NAME, s: ""},
}

func defaultValue(typ v1beta1.ValueType) ref.Val {
	if out, ok := defaultValues[typ]; ok {
		return out
	}
	return types.NewErr("cannot provide defaults for %q", typ)
}

var (
	stringMapType = &exprpb.Type{TypeKind: &exprpb.Type_MapType_{MapType: &exprpb.Type_MapType{
		KeyType:   &exprpb.Type{TypeKind: &exprpb.Type_Primitive{Primitive: exprpb.Type_STRING}},
		ValueType: &exprpb.Type{TypeKind: &exprpb.Type_Primitive{Primitive: exprpb.Type_STRING}},
	}}}
	emptyStringMap = stringMapValue{value: attribute.WrapStringMap(nil)}

	// domain specific types do not implement any of type traits for now
	wrapperType = types.NewTypeValue("wrapper")
)

const (
	ipAddressType    = "istio.policy.v1beta1.IPAddress"
	emailAddressType = "istio.policy.v1beta1.EmailAddress"
	uriType          = "istio.policy.v1beta1.Uri"
	dnsType          = "istio.policy.v1beta1.DNSName"
)

type stringMapValue struct {
	value attribute.StringMap
}

func (v stringMapValue) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	return nil, errors.New("cannot convert stringmap to native types")
}
func (v stringMapValue) ConvertToType(typeValue ref.Type) ref.Val {
	return types.NewErr("cannot convert stringmap to CEL types")
}
func (v stringMapValue) Equal(other ref.Val) ref.Val {
	return types.NewErr("stringmap does not support equality")
}
func (v stringMapValue) Type() ref.Type {
	return types.MapType
}
func (v stringMapValue) Value() interface{} {
	return v.value
}
func (v stringMapValue) Get(index ref.Val) ref.Val {
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
func (v stringMapValue) Contains(index ref.Val) ref.Val {
	if index.Type() != types.StringType {
		return types.NewErr("index should be a string")
	}

	field := index.Value().(string)
	_, found := v.value.Get(field)
	return types.Bool(found)
}
func (v stringMapValue) Size() ref.Val {
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
func (v wrapperValue) ConvertToType(typeValue ref.Type) ref.Val {
	return types.NewErr("cannot convert wrapper value  to CEL types")
}
func (v wrapperValue) Equal(other ref.Val) ref.Val {
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
