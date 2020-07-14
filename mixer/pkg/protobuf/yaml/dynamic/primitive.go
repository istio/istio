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

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/pkg/errors"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/lang/compiled"
)

type encoderFn func(bag attribute.Bag, ba []byte) ([]byte, error)
type encoderDirectFn func(v interface{}, etype descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error)
type buildEEncoderFn func(expr compiled.Expression) encoderFn

// utilities and encoder related to expression evaluation.
type eEncoderRegistryEntry struct {
	name    string
	build   buildEEncoderFn
	encode  encoderDirectFn
	accepts v1beta1.ValueType
	encodes []descriptor.FieldDescriptorProto_Type
}

// 19 is the largest value for FieldDescriptorProto_Type.
var eEncoderRegistry = make([]*eEncoderRegistryEntry, 19)

func registerEncoder(name string, encodes []descriptor.FieldDescriptorProto_Type,
	accepts v1beta1.ValueType, build buildEEncoderFn, encode encoderDirectFn) {
	ent := &eEncoderRegistryEntry{
		name:    name,
		encodes: encodes,
		accepts: accepts,
		build:   build,
		encode:  encode,
	}
	for _, en := range encodes {
		eEncoderRegistry[en] = ent
	}
}

func init() {
	registerEncoder("string", []descriptor.FieldDescriptorProto_Type{
		descriptor.FieldDescriptorProto_TYPE_STRING},
		v1beta1.STRING,
		func(expr compiled.Expression) encoderFn {
			return func(bag attribute.Bag, ba []byte) ([]byte, error) {
				v, err := expr.EvaluateString(bag)
				if err != nil {
					return nil, err
				}
				return encodeString(v, ba)
			}
		},
		func(v interface{}, _ descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
			return EncodeString(v, ba)
		})
}

func init() {
	registerEncoder("float", []descriptor.FieldDescriptorProto_Type{
		descriptor.FieldDescriptorProto_TYPE_FLOAT},
		v1beta1.DOUBLE,
		func(expr compiled.Expression) encoderFn {
			return func(bag attribute.Bag, ba []byte) ([]byte, error) {
				v, err := expr.EvaluateDouble(bag)
				if err != nil {
					return nil, err
				}
				return EncodeFloat(v, ba)
			}
		},
		func(v interface{}, _ descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
			return EncodeFloat(v, ba)
		})
}

func init() {
	registerEncoder("double", []descriptor.FieldDescriptorProto_Type{
		descriptor.FieldDescriptorProto_TYPE_DOUBLE},
		v1beta1.DOUBLE,
		func(expr compiled.Expression) encoderFn {
			return func(bag attribute.Bag, ba []byte) ([]byte, error) {
				v, err := expr.EvaluateDouble(bag)
				if err != nil {
					return nil, err
				}
				return EncodeDouble(v, ba)
			}
		},
		func(v interface{}, _ descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
			return EncodeDouble(v, ba)
		})
}

func init() {
	registerEncoder("int", []descriptor.FieldDescriptorProto_Type{
		descriptor.FieldDescriptorProto_TYPE_INT32, descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_UINT32, descriptor.FieldDescriptorProto_TYPE_UINT64},
		v1beta1.INT64,
		func(expr compiled.Expression) encoderFn {
			return func(bag attribute.Bag, ba []byte) ([]byte, error) {
				v, err := expr.EvaluateInteger(bag)
				if err != nil {
					return nil, err
				}
				return encodeInt(v, ba)
			}
		},
		func(v interface{}, _ descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
			return EncodeInt(v, ba)
		})
}

func init() {
	registerEncoder("fixed64", []descriptor.FieldDescriptorProto_Type{
		descriptor.FieldDescriptorProto_TYPE_SFIXED64, descriptor.FieldDescriptorProto_TYPE_FIXED64},
		v1beta1.INT64,
		func(expr compiled.Expression) encoderFn {
			return func(bag attribute.Bag, ba []byte) ([]byte, error) {
				v, err := expr.EvaluateInteger(bag)
				if err != nil {
					return nil, err
				}
				return encodeFixed64(ba, uint64(v)), nil
			}
		},
		func(v interface{}, _ descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
			return EncodeFixed64(v, ba)
		})
}

func init() {
	registerEncoder("fixed32", []descriptor.FieldDescriptorProto_Type{
		descriptor.FieldDescriptorProto_TYPE_SFIXED32, descriptor.FieldDescriptorProto_TYPE_FIXED32},
		v1beta1.INT64,
		func(expr compiled.Expression) encoderFn {
			return func(bag attribute.Bag, ba []byte) ([]byte, error) {
				v, err := expr.EvaluateInteger(bag)
				if err != nil {
					return nil, err
				}
				return encodeFixed32(ba, uint64(v)), nil
			}
		},
		func(v interface{}, _ descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
			return EncodeFixed32(v, ba)
		})
}

func init() {
	registerEncoder("bool", []descriptor.FieldDescriptorProto_Type{
		descriptor.FieldDescriptorProto_TYPE_BOOL},
		v1beta1.BOOL,
		func(expr compiled.Expression) encoderFn {
			return func(bag attribute.Bag, ba []byte) ([]byte, error) {
				v, err := expr.EvaluateBoolean(bag)
				if err != nil {
					return nil, err
				}
				return encodeBool(v, ba), nil
			}
		},
		func(v interface{}, _ descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
			return EncodeBool(v, ba)
		})
}

func init() {
	registerEncoder("sint32", []descriptor.FieldDescriptorProto_Type{
		descriptor.FieldDescriptorProto_TYPE_SINT32},
		v1beta1.INT64,
		func(expr compiled.Expression) encoderFn {
			return func(bag attribute.Bag, ba []byte) ([]byte, error) {
				v, err := expr.EvaluateInteger(bag)
				if err != nil {
					return nil, err
				}
				return encodeZigzag32(ba, uint64(v)), nil
			}
		},
		func(v interface{}, _ descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
			return EncodeSInt32(v, ba)
		})
}

func init() {
	registerEncoder("sint64", []descriptor.FieldDescriptorProto_Type{
		descriptor.FieldDescriptorProto_TYPE_SINT64},
		v1beta1.INT64,
		func(expr compiled.Expression) encoderFn {
			return func(bag attribute.Bag, ba []byte) ([]byte, error) {
				v, err := expr.EvaluateInteger(bag)
				if err != nil {
					return nil, err
				}
				return encodeZigzag64(ba, uint64(v)), nil
			}
		},
		func(v interface{}, _ descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
			return EncodeSInt64(v, ba)
		})
}

// Enum encoding on string.
type eEnumEncoder struct {
	expr       compiled.Expression
	enumValues []*descriptor.EnumValueDescriptorProto
}

func (e eEnumEncoder) Encode(bag attribute.Bag, ba []byte) ([]byte, error) {
	v, err := e.expr.Evaluate(bag)
	if err != nil {
		return nil, err
	}
	return EncodeEnum(v, ba, e.enumValues)
}

type primEvalEncoder struct {
	name string
	enc  encoderFn
}

func (p primEvalEncoder) Encode(bag attribute.Bag, ba []byte) ([]byte, error) {
	return p.enc(bag, ba)
}

// BuildPrimitiveEvalEncoder returns an eval encoder given an expression and a target fieldEncoder
func BuildPrimitiveEvalEncoder(expr compiled.Expression, vt v1beta1.ValueType, fld *descriptor.FieldDescriptorProto) (Encoder, error) {
	enc := eEncoderRegistry[fld.GetType()]
	if enc == nil {
		return nil, fmt.Errorf("do not know how to encode %v", *fld.Type)
	}

	if enc.accepts != vt {
		return nil, fmt.Errorf("encoder %s for type %v does not accept %v", enc.name, *fld.Type, vt)
	}

	return &primEvalEncoder{
		name: enc.name,
		enc:  enc.build(expr),
	}, nil
}

// staticEncoder for pre-encoded data
type staticEncoder struct {
	name          string
	encodedData   []byte
	includeLength bool
}

func (p staticEncoder) Encode(_ attribute.Bag, ba []byte) ([]byte, error) {
	if p.includeLength {
		ba, _ = EncodeVarint(ba, uint64(len(p.encodedData)))
	}
	return append(ba, p.encodedData...), nil
}

// BuildPrimitiveEncoder encodes the given data and returns a static encoder.
func BuildPrimitiveEncoder(v interface{}, fld *descriptor.FieldDescriptorProto) (Encoder, error) {
	enc := eEncoderRegistry[*fld.Type]
	if enc == nil {
		return nil, fmt.Errorf("do not know how to encode %v", *fld.Type)
	}

	pe := &staticEncoder{
		name: fld.GetName(),
	}

	var err error
	if pe.encodedData, err = enc.encode(v, *fld.Type, pe.encodedData); err != nil {
		return nil, err
	}

	return pe, nil
}

// staticAttributeEncoder for pre-encoded data delivered via attribute bag
type staticAttributeEncoder struct {
	// name of the attribute to read from
	attrName string
}

func (p staticAttributeEncoder) Encode(a attribute.Bag, ba []byte) ([]byte, error) {
	v, ok := a.Get(p.attrName)
	if !ok {
		return nil, errors.Errorf("unable to find attribute: %s", p.attrName)
	}
	var ea []byte
	if ea, ok = v.([]byte); !ok {
		return nil, errors.Errorf("unexpected attribute (%s) type. got %T, want []byte", p.attrName, v)
	}
	ba, _ = EncodeVarint(ba, uint64(len(ea)))
	return append(ba, ea...), nil
}
