// Copyright Istio Authors.
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

package dynamic

import (
	"fmt"
	"net"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/types"
	"github.com/pkg/errors"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/runtime/lang"
)

// valueTypeName is the Name of the value type with special input encoding
const valueTypeName = ".istio.policy.v1beta1.Value"

func valueTypeEncoderBuilder(_ *descriptor.DescriptorProto, fd *descriptor.FieldDescriptorProto, v interface{}, compiler lang.Compiler) (Encoder, error) {
	_, supported := vtRegistryByMsgName[fd.GetTypeName()]
	if fd.GetTypeName() != valueTypeName && !supported {
		return nil, fmt.Errorf("cannot process message of type:%s", fd.GetTypeName())
	}

	var vVal v1beta1.Value
	switch vv := v.(type) {
	case int:
		vVal.Value = &v1beta1.Value_Int64Value{Int64Value: int64(vv)}
	case float64:
		vVal.Value = &v1beta1.Value_DoubleValue{DoubleValue: vv}
	case bool:
		vVal.Value = &v1beta1.Value_BoolValue{BoolValue: vv}
	case string:
		val, isConstString := transformQuotedString(vv)
		sval, _ := val.(string)
		if isConstString {
			vVal.Value = &v1beta1.Value_StringValue{StringValue: sval}
		} else {
			return buildExprEncoder(sval, fd.GetTypeName(), compiler)
		}
	default:
		return nil, fmt.Errorf("unsupported type: %T of %v", v, v)
	}

	encodedData := make([]byte, 0, vVal.Size()+2)
	var err error
	if encodedData, err = marshalValWithSize(&vVal, encodedData); err != nil {
		return nil, err
	}

	return &staticEncoder{
		name:        fd.GetName(),
		encodedData: encodedData,
	}, nil
}

func buildExprEncoder(sval string, msgType string, compiler lang.Compiler) (Encoder, error) {
	var expr compiled.Expression
	var vt v1beta1.ValueType
	var err error

	if expr, vt, err = compiler.Compile(sval); err != nil {
		return nil, err
	}

	var bld valueProducer
	var re *vtRegistryEntry
	var found bool

	if msgType == valueTypeName {
		re, found = vtRegistry[vt]
	} else {
		re, found = vtRegistryByMsgName[msgType]
		if re.accepts != vt {
			return nil, fmt.Errorf("incorrect type for %s. got: %v, want: %v", msgType, vt, re.accepts)
		}
	}
	if !found {
		return nil, fmt.Errorf("unsupported type: %v (%s)", vt, msgType)
	}

	if msgType != valueTypeName && msgType != re.name {
		return nil, fmt.Errorf("incorrect message type for %s. got: %s, want: %s", vt, msgType, re.name)
	}

	bld, err = re.build(expr, vt)

	if err != nil {
		return nil, err
	}

	return &valueTypeDynamicEncoder{buildValue: bld, wrap: msgType == valueTypeName}, nil
}

type valueTypeDynamicEncoder struct {
	buildValue valueProducer
	wrap       bool
}

func (vv valueTypeDynamicEncoder) Encode(bag attribute.Bag, ba []byte) ([]byte, error) {
	vVal, err := vv.buildValue(bag, vv.wrap)
	if err != nil {
		return nil, err
	}
	return marshalValWithSize(vVal, ba)
}

func marshalValWithSize(vVal marshaler, ba []byte) ([]byte, error) {
	msgSize := vVal.Size()
	ba, _ = EncodeVarint(ba, uint64(msgSize))
	startIdx := len(ba)
	ba = extendSlice(ba, msgSize)
	_, err := vVal.MarshalTo(ba[startIdx:])
	return ba, err
}

func incorrectTypeError(vt v1beta1.ValueType, got interface{}, want string) error {
	return fmt.Errorf("incorrect type for %v. got: %T, want: %s", vt, got, want)
}

// gogo codegen generates code that conforms to marshaler interface.
type marshaler interface {
	MarshalTo([]byte) (int, error)
	Size() int
}

// valueProducer produces specific .istio.policy.v1beta1.XXX_Value given an attribute bag.
// If wrap is specified, the value is wrapped in a ValueType
type valueProducer func(bag attribute.Bag, wrap bool) (marshaler, error)

// valueProducerBuilder creates builder of a predefined type based on the given expression.
type valueProducerBuilder func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error)

type vtRegistryEntry struct {
	name    string
	build   valueProducerBuilder
	accepts v1beta1.ValueType
}

var vtRegistryByMsgName = make(map[string]*vtRegistryEntry)
var vtRegistry = make(map[v1beta1.ValueType]*vtRegistryEntry)

func registerVT(msgname string, accepts v1beta1.ValueType, build valueProducerBuilder) {
	name := "." + msgname
	ent := &vtRegistryEntry{
		name:    name,
		accepts: accepts,
		build:   build,
	}
	vtRegistryByMsgName[name] = ent
	vtRegistry[accepts] = ent
}

func init() {
	registerVT("bool", v1beta1.BOOL,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag, _ bool) (marshaler, error) {
				ev, er := expr.EvaluateBoolean(bag)
				if er != nil {
					return nil, er
				}
				v := &v1beta1.Value_BoolValue{BoolValue: ev}
				return &v1beta1.Value{Value: v}, nil

			}, nil
		})
	registerVT("int64", v1beta1.INT64,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag, _ bool) (marshaler, error) {
				ev, er := expr.EvaluateInteger(bag)
				if er != nil {
					return nil, er
				}
				v := &v1beta1.Value_Int64Value{Int64Value: ev}
				return &v1beta1.Value{Value: v}, nil

			}, nil
		})
	registerVT("double", v1beta1.DOUBLE,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag, _ bool) (marshaler, error) {
				ev, er := expr.EvaluateDouble(bag)
				if er != nil {
					return nil, er
				}
				v := &v1beta1.Value_DoubleValue{DoubleValue: ev}
				return &v1beta1.Value{Value: v}, nil

			}, nil
		})
	registerVT("string", v1beta1.STRING,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag, _ bool) (marshaler, error) {
				ev, er := expr.EvaluateString(bag)
				if er != nil {
					return nil, er
				}
				v := &v1beta1.Value_StringValue{StringValue: ev}
				return &v1beta1.Value{Value: v}, nil

			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.Uri)(nil)), v1beta1.URI,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag, wrap bool) (marshaler, error) {
				ev, er := expr.EvaluateString(bag)
				if er != nil {
					return nil, er
				}
				v := &v1beta1.Uri{Value: ev}
				if !wrap {
					return v, nil
				}
				return &v1beta1.Value{Value: &v1beta1.Value_UriValue{UriValue: v}}, nil
			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.DNSName)(nil)), v1beta1.DNS_NAME,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag, wrap bool) (marshaler, error) {
				ev, er := expr.EvaluateString(bag)
				if er != nil {
					return nil, er
				}
				v := &v1beta1.DNSName{Value: ev}
				if !wrap {
					return v, nil
				}
				return &v1beta1.Value{Value: &v1beta1.Value_DnsNameValue{DnsNameValue: v}}, nil
			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.EmailAddress)(nil)), v1beta1.EMAIL_ADDRESS,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag, wrap bool) (marshaler, error) {
				ev, er := expr.EvaluateString(bag)
				if er != nil {
					return nil, er
				}
				v := &v1beta1.EmailAddress{Value: ev}
				if !wrap {
					return v, nil
				}
				return &v1beta1.Value{Value: &v1beta1.Value_EmailAddressValue{
					EmailAddressValue: v}}, nil
			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.IPAddress)(nil)), v1beta1.IP_ADDRESS,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag, wrap bool) (marshaler, error) {
				ev, er := expr.Evaluate(bag)
				if er != nil {
					return nil, er
				}
				var ok bool
				var ip net.IP

				if ip, ok = ev.([]byte); !ok {
					return nil, incorrectTypeError(vt, ev, "[]byte")
				}
				v := &v1beta1.IPAddress{Value: ip}
				if !wrap {
					return v, nil
				}
				return &v1beta1.Value{Value: &v1beta1.Value_IpAddressValue{IpAddressValue: v}}, nil
			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.Duration)(nil)), v1beta1.DURATION,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag, wrap bool) (marshaler, error) {
				ev, er := expr.Evaluate(bag)
				if er != nil {
					return nil, er
				}
				// for expression of type v1beta1.DURATION,
				// expr.Evaluate ensures that only time.Duration is returned.
				// safe to cast
				v := &v1beta1.Duration{Value: types.DurationProto(ev.(time.Duration))}
				if !wrap {
					return v, nil
				}

				return &v1beta1.Value{Value: &v1beta1.Value_DurationValue{DurationValue: v}}, nil
			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.TimeStamp)(nil)), v1beta1.TIMESTAMP,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag, wrap bool) (marshaler, error) {
				ev, er := expr.Evaluate(bag)
				if er != nil {
					return nil, er
				}
				var ok bool
				var t time.Time

				if t, ok = ev.(time.Time); !ok {
					return nil, incorrectTypeError(vt, ev, "time.Time")
				}

				ts, er := types.TimestampProto(t)
				if er != nil {
					return nil, errors.Errorf("invalid timestamp: %v", er)
				}
				v := &v1beta1.TimeStamp{Value: ts}
				if !wrap {
					return v, nil
				}
				return &v1beta1.Value{Value: &v1beta1.Value_TimestampValue{TimestampValue: v}}, nil
			}, nil
		})
}

// valueTypeBuilderFuncs returns named builders for all
func valueTypeBuilderFuncs() map[string]NamedEncoderBuilderFunc {
	m := map[string]NamedEncoderBuilderFunc{
		valueTypeName: valueTypeEncoderBuilder,
	}

	for msgType := range vtRegistryByMsgName {
		m[msgType] = valueTypeEncoderBuilder
	}
	return m
}
