// Copyright 2018 Istio Authors.
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
)

// valueTypeName is the Name of the value type with special input encoding
const valueTypeName = ".istio.policy.v1beta1.Value"

func valueTypeEncoderBuilder(_ *descriptor.DescriptorProto, fd *descriptor.FieldDescriptorProto, v interface{}, compiler Compiler) (Encoder, error) {
	if fd.GetTypeName() != valueTypeName {
		return nil, fmt.Errorf("cannot process message of type:%s", fd.GetTypeName())
	}

	var vVal v1beta1.Value
	switch vv := v.(type) {
	case int:
		vVal.Value = &v1beta1.Value_Int64Value{int64(vv)}
	case float64:
		vVal.Value = &v1beta1.Value_DoubleValue{vv}
	case bool:
		vVal.Value = &v1beta1.Value_BoolValue{vv}
	case string:
		val, isConstString := transformQuotedString(vv)
		sval, _ := val.(string)
		if isConstString {
			vVal.Value = &v1beta1.Value_StringValue{sval}
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

func buildExprEncoder(sval string, msgType string, compiler Compiler) (Encoder, error) {
	var expr compiled.Expression
	var vt v1beta1.ValueType
	var err error

	if expr, vt, err = compiler.Compile(sval); err != nil {
		return nil, err
	}

	var bld valueProducer
	re, ok := vtRegistry[vt]
	if !ok {
		return nil, fmt.Errorf("unsupported type: %v", vt)
	}

	bld, err = re.build(expr, vt)

	if err != nil {
		return nil, err
	}

	return &valueTypeDynamicEncoder{buildValue: bld}, nil
}

type valueTypeDynamicEncoder struct {
	buildValue valueProducer
}

func (vv valueTypeDynamicEncoder) Encode(bag attribute.Bag, ba []byte) ([]byte, error) {
	vVal, err := vv.buildValue(bag)
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
type valueProducer func(bag attribute.Bag) (*v1beta1.Value, error)

// valueProducerBuilder creates builder of a predefined type based on the given expression.
type valueProducerBuilder func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error)

type vtRegistryEntry struct {
	name    string
	build   valueProducerBuilder
	accepts v1beta1.ValueType
}

var vtRegistryByMsgName = make(map[string]*vtRegistryEntry)
var vtRegistry = make(map[v1beta1.ValueType]*vtRegistryEntry)

func registerVT(name string, accepts v1beta1.ValueType, build valueProducerBuilder) {
	ent := &vtRegistryEntry{
		name:    name,
		accepts: accepts,
		build:   build,
	}
	vtRegistryByMsgName[name] = ent
	vtRegistry[accepts] = ent
}

func init() {
	registerVT(".bool", v1beta1.BOOL,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag) (*v1beta1.Value, error) {
				ev, er := expr.EvaluateBoolean(bag)
				if er != nil {
					return nil, er
				}
				return &v1beta1.Value{Value: &v1beta1.Value_BoolValue{BoolValue: ev}}, nil

			}, nil
		})
	registerVT(".int64", v1beta1.INT64,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag) (*v1beta1.Value, error) {
				ev, er := expr.EvaluateInteger(bag)
				if er != nil {
					return nil, er
				}
				return &v1beta1.Value{Value: &v1beta1.Value_Int64Value{Int64Value: ev}}, nil

			}, nil
		})
	registerVT(".double", v1beta1.DOUBLE,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag) (*v1beta1.Value, error) {
				ev, er := expr.EvaluateDouble(bag)
				if er != nil {
					return nil, er
				}
				return &v1beta1.Value{Value: &v1beta1.Value_DoubleValue{DoubleValue: ev}}, nil

			}, nil
		})
	registerVT(".string", v1beta1.STRING,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag) (*v1beta1.Value, error) {
				ev, er := expr.EvaluateString(bag)
				if er != nil {
					return nil, er
				}
				return &v1beta1.Value{Value: &v1beta1.Value_StringValue{StringValue: ev}}, nil

			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.Uri)(nil)), v1beta1.URI,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag) (*v1beta1.Value, error) {
				ev, er := expr.EvaluateString(bag)
				if er != nil {
					return nil, er
				}
				return &v1beta1.Value{Value: &v1beta1.Value_UriValue{
					UriValue: &v1beta1.Uri{Value: ev}}}, nil
			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.DNSName)(nil)), v1beta1.DNS_NAME,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag) (*v1beta1.Value, error) {
				ev, er := expr.EvaluateString(bag)
				if er != nil {
					return nil, er
				}
				return &v1beta1.Value{Value: &v1beta1.Value_DnsNameValue{
					DnsNameValue: &v1beta1.DNSName{Value: ev}}}, nil
			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.EmailAddress)(nil)), v1beta1.EMAIL_ADDRESS,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag) (*v1beta1.Value, error) {
				ev, er := expr.EvaluateString(bag)
				if er != nil {
					return nil, er
				}
				return &v1beta1.Value{Value: &v1beta1.Value_EmailAddressValue{
					EmailAddressValue: &v1beta1.EmailAddress{Value: ev}}}, nil
			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.IPAddress)(nil)), v1beta1.IP_ADDRESS,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag) (*v1beta1.Value, error) {
				ev, er := expr.Evaluate(bag)
				if er != nil {
					return nil, er
				}
				var ok bool
				var v net.IP

				if v, ok = ev.(net.IP); !ok {
					return nil, incorrectTypeError(vt, ev, "[]byte")
				}
				return &v1beta1.Value{Value: &v1beta1.Value_IpAddressValue{IpAddressValue: &v1beta1.IPAddress{Value: v}}}, nil
			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.Duration)(nil)), v1beta1.DURATION,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag) (*v1beta1.Value, error) {
				ev, er := expr.Evaluate(bag)
				if er != nil {
					return nil, er
				}
				// for expression of type v1beta1.DURATION,
				// expr.Evaluate ensures that only time.Duration is returned.
				// safe to cast
				v := ev.(time.Duration)

				return &v1beta1.Value{Value: &v1beta1.Value_DurationValue{
					DurationValue: &v1beta1.Duration{Value: types.DurationProto(v)}}}, nil
			}, nil
		})
	registerVT(proto.MessageName((*v1beta1.TimeStamp)(nil)), v1beta1.TIMESTAMP,
		func(expr compiled.Expression, vt v1beta1.ValueType) (valueProducer, error) {
			return func(bag attribute.Bag) (*v1beta1.Value, error) {
				ev, er := expr.Evaluate(bag)
				if er != nil {
					return nil, er
				}
				var ok bool
				var v time.Time

				if v, ok = ev.(time.Time); !ok {
					return nil, incorrectTypeError(vt, ev, "time.Time")
				}

				ts, er := types.TimestampProto(v)
				if er != nil {
					return nil, errors.Errorf("invalid timestamp: %v", er)
				}

				return &v1beta1.Value{Value: &v1beta1.Value_TimestampValue{TimestampValue: &v1beta1.TimeStamp{Value: ts}}}, nil
			}, nil
		})
}
