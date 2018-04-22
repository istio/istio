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
	"sort"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/protobuf/yaml"
)

type (
	// Encoder transforms yaml that represents protobuf data into []byte
	// The yaml representation may have dynamic content
	Encoder interface {
		Encode(bag attribute.Bag, ba []byte) ([]byte, error)
	}

	messageEncoder struct {
		// skipEncodeLength skip encoding length of the message in the output
		// should be true only for top level message.
		skipEncodeLength bool

		// fields of the message.
		fields []*field
	}

	field struct {
		// proto key  -- EncodeVarInt ((field_number << 3) | wire_type)
		protoKey []byte

		// encodedData is available if the entire field can be encoded
		// at compile time.
		encodedData []byte

		// encoder is needed if encodedData is not available.
		// packed fields have a list of encoders.
		encoder []Encoder

		// number fields are sorted by field number.
		number int
		// name for debug.
		name string
		// if packed this is set to true
		packed bool
	}

	EncoderBuilder struct {
		msgName     string
		resolver    yaml.Resolver
		data        map[interface{}]interface{}
		compiler    compiled.Compiler
		skipUnknown bool
	}
)

// NewEncoderBuilder creates a EncoderBuilder.
func NewEncoderBuilder(msgName string, resolver yaml.Resolver, data map[interface{}]interface{},
	compiler compiled.Compiler, skipUnknown bool) *EncoderBuilder {
	return &EncoderBuilder{
		msgName:     msgName,
		resolver:    resolver,
		data:        data,
		compiler:    compiler,
		skipUnknown: skipUnknown}
}

// Build builds a DynamicEncoder
func (c EncoderBuilder) Build() (Encoder, error) {
	m := c.resolver.ResolveMessage(c.msgName)
	if m == nil {
		return nil, fmt.Errorf("cannot resolve message '%s'", c.msgName)
	}

	return c.buildMessage(m, c.data, true)
}

func (c EncoderBuilder) buildMessage(md *descriptor.DescriptorProto, data map[interface{}]interface{}, skipEncodeLength bool) (Encoder, error) {
	var err error
	var ok bool

	me := messageEncoder{
		skipEncodeLength: skipEncodeLength,
	}
	isMap := md.GetOptions().GetMapEntry()

	keys := make([]string, 0, len(data))

	for kk := range data {
		var k string
		if k, ok = kk.(string); !ok {
			return nil, fmt.Errorf("error processing message '%s':%v got %T want string", md.GetName(), kk, kk)
		}
		keys = append(keys, k)
	}

	// This is off the data path.
	// This way we get consistent log output.

	sort.Strings(keys)

	for _, k := range keys {
		v := data[k]
		fd := yaml.FindFieldByName(md, k)
		if fd == nil {
			if c.skipUnknown {
				continue
			}
			return nil, fmt.Errorf("field '%s' not found in message '%s'", k, md.GetName())
		}

		repeated := fd.IsRepeated()
		packed := fd.IsPacked() || fd.IsPacked3()

		switch fd.GetType() {
		// primitives
		case descriptor.FieldDescriptorProto_TYPE_DOUBLE,
			descriptor.FieldDescriptorProto_TYPE_FLOAT,
			descriptor.FieldDescriptorProto_TYPE_INT64,
			descriptor.FieldDescriptorProto_TYPE_UINT64,
			descriptor.FieldDescriptorProto_TYPE_INT32,
			descriptor.FieldDescriptorProto_TYPE_FIXED64,
			descriptor.FieldDescriptorProto_TYPE_FIXED32,
			descriptor.FieldDescriptorProto_TYPE_BOOL,
			descriptor.FieldDescriptorProto_TYPE_UINT32,
			descriptor.FieldDescriptorProto_TYPE_SFIXED32,
			descriptor.FieldDescriptorProto_TYPE_SFIXED64,
			descriptor.FieldDescriptorProto_TYPE_SINT32,
			descriptor.FieldDescriptorProto_TYPE_SINT64,
			descriptor.FieldDescriptorProto_TYPE_STRING:
			if repeated && !packed {
				return nil, fmt.Errorf("unpacked primitives not supported")
			}

			var fld *field
			if fld, err = c.handlePrimitiveField(v, fd, &me, isMap && k == "key"); err != nil {
				return nil, fmt.Errorf("unable to encode field: %v. %v", fd, err)
			}
			me.fields = append(me.fields, fld)

		case descriptor.FieldDescriptorProto_TYPE_ENUM:
			if repeated && !packed {
				return nil, fmt.Errorf("unpacked primitives not supported")
			}
			var constString bool
			var expr compiled.Expression
			var vt v1beta1.ValueType

			v, constString = transFormQuotedString(v)

			if !(isMap && k == "key") && !constString { // do not use dynamic keys in maps.
				expr, vt, err = ExtractExpression(v, c.compiler)
			}

			_ = vt

			if err != nil {
				return nil, fmt.Errorf("failed to process %v:%v %v", k, v, err)
			}


			fld := makeField(fd)
			e := c.resolver.ResolveEnum(fd.GetTypeName())
			if expr != nil {
				fld.encoder = []Encoder{&eEnumEncoder{
					expr:       expr,
					enumValues: e.Value,
				}}
			} else if vs, ok := v.(string); ok {
				if fld.encodedData, err = EncodeEnumString(vs, fld.encodedData, e.Value); err != nil {
					return nil, fmt.Errorf("unable to encode: %v. %v", k, err)
				}
			}
			me.fields = append(me.fields, fld)

		case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
			m := c.resolver.ResolveMessage(fd.GetTypeName())
			if m == nil {
				return nil, fmt.Errorf("unable to resolve message '%s'", fd.GetTypeName())
			}

			var ma []interface{}
			if m.GetOptions().GetMapEntry() { // this is a Map
				ma, err = convertMapToMapentry(v)
				if err != nil {
					return nil, fmt.Errorf("unable to process: %v, %v", fd, err)
				}
			} else if repeated { // map entry is always repeated.
				ma, ok = v.([]interface{})
				if !ok {
					return nil, fmt.Errorf("unable to process: %v, got %T, want: []interface{}", fd, v)
				}
			} else {
				ma = []interface{}{v}
			}

			// now maps, messages and repeated messages all look the same.
			if err = c.handleMessageField(ma, m, fd, &me); err != nil {
				return nil, err
			}

		default:
			return nil, fmt.Errorf("field not supported '%v'", fd)
		}
	}

	// sorting is recommended
	sort.Slice(me.fields, func(i, j int) bool {
		return me.fields[i].number < me.fields[j].number
	})

	return me, nil
}

// handlePrimitiveField handles encoding of single and repeated packed primitives
func (c EncoderBuilder) handlePrimitiveField(v interface{}, fd *descriptor.FieldDescriptorProto,
		me1 *messageEncoder, static bool) (*field, error) {
	fld := makeField(fd)

	va, packed := v.([]interface{})
	if !packed {
		va = []interface{}{v}
	}

	for _, vl := range va {
		var err error
		var enc Encoder

		val, isConstString := transFormQuotedString(vl)
		sval, isString := val.(string)

		// if compiler is nil, everything is considered static
		if static || isConstString || !isString || c.compiler == nil{
			if enc, err = PrimitiveEncoder(val, fd); err != nil {
				return nil, fmt.Errorf("unable to build primitive encoder: %v. %v", val, err)
			}
		} else {
			var expr compiled.Expression
			var vt v1beta1.ValueType

			if expr, vt, err = c.compiler.Compile(sval); err != nil {
				return nil, err
			}
			if enc, err = PrimitiveEvalEncoder(expr, vt, fd); err != nil {
				return nil, fmt.Errorf("unable to build eval encoder: %v. %v", val, err)
			}
		}

		fld.encoder = append(fld.encoder, enc)
	}

	return fld, nil
}

func (c EncoderBuilder) handleMessageField(ma []interface{}, m *descriptor.DescriptorProto,
	fd *descriptor.FieldDescriptorProto, me *messageEncoder) error {

	for _, vv := range ma {
		var ok bool

		var vq map[interface{}]interface{}
		if vq, ok = vv.(map[interface{}]interface{}); !ok {
			return fmt.Errorf("unable to process: %v, got %T, want: map[string]interface{}", fd, vv)
		}

		var de Encoder
		var err error
		if de, err = c.buildMessage(m, vq, false); err != nil {
			return fmt.Errorf("unable to process: %v, %v", fd, err)
		}
		fld := makeField(fd)
		fld.encoder = []Encoder{de}
		me.fields = append(me.fields, fld)
	}

	return nil
}

func convertMapToMapentry(data interface{}) ([]interface{}, error) {
	md, ok := data.(map[interface{}]interface{})
	if !ok {
		return nil, fmt.Errorf("incorrect map type:%T, want:map[interface{}]interface{}", data)
	}
	res := make([]interface{}, 0, len(md))
	for k, v := range md {
		res = append(res, map[interface{}]interface{}{
			"key":   k,
			"value": v,
		})
	}
	return res, nil
}

func extendSlice(ba []byte, n int) []byte {
	for k := 0; k < n; k++ {
		ba = append(ba, 0xff)
	}
	return ba
}

// expected length of the varint encoded word
// 2 byte words represent 2 ** 14 = 16K bytes
// If message length is more, it involves an array copy
const varLength = 2

// encode message including length of the message into []byte
func (m messageEncoder) Encode(bag attribute.Bag, ba []byte) ([]byte, error) {
	var err error

	if m.skipEncodeLength {
		return m.encodeWithoutLength(bag, ba)
	}

	l0 := len(ba)
	// #pragma inline reserve varLength bytes
	ba = extendSlice(ba, varLength)
	l1 := len(ba)

	if ba, err = m.encodeWithoutLength(bag, ba); err != nil {
		return nil, err
	}

	length := len(ba) - l1
	diff := proto.SizeVarint(uint64(length)) - varLength
	// move data forward because we need more than varLength bytes
	if diff > 0 {
		ba = extendSlice(ba, diff)
		// shift data down. This should rarely occur.
		copy(ba[l1+diff:], ba[l1:])
	}

	// ignore return value. EncodeLength is writing in the middle of the array.
	_ = EncodeVarintZeroExtend(ba[l0:l0], uint64(length), varLength)

	return ba, nil
}

func (m messageEncoder) encodeWithoutLength(bag attribute.Bag, ba []byte) ([]byte, error) {
	var err error
	for _, f := range m.fields {
		ba, err = f.Encode(bag, ba)
		if err != nil {
			return nil, fmt.Errorf("field: %s - %v", f.name, err)
		}
	}
	return ba, nil
}


type evalEncoder struct {
	//TODO handle google.proto.Value type
	useValueType bool
	etype        descriptor.FieldDescriptorProto_Type
	ex           compiled.Expression
}

func (e evalEncoder) Encode(bag attribute.Bag, ba []byte) ([]byte, error) {
	v, err := e.ex.Evaluate(bag)
	if err != nil {
		return nil, err
	}

	return EncodePrimitive(v, e.etype, ba)
}

func (f field) Encode(bag attribute.Bag, ba []byte) ([]byte, error) {
	if f.protoKey != nil {
		ba = append(ba, f.protoKey...)
	}

	// if data was fully encoded just use it.
	if f.encodedData != nil {
		return append(ba, f.encodedData...), nil
	}

	// The following call happens when
	// 1. value requires expression evaluation.
	// 2. field is of type map
	// 3. field is of Message
	// In all cases Encode function must correctly set Length.

	var l0 int
	var l1 int

	if f.packed {
		l0 = len(ba)
		// #pragma inline reserve varLength bytes
		ba = extendSlice(ba, varLength)
		l1 = len(ba)
	}

	var err error
	for _, en := range f.encoder {
		ba, err = en.Encode(bag, ba)
		if err != nil {
			return nil, err
		}
	}

	if f.packed {
		length := len(ba) - l1
		diff := proto.SizeVarint(uint64(length)) - varLength
		// move data forward because we need more than varLength bytes
		if diff > 0 {
			ba = extendSlice(ba, diff)
			// shift data down. This should rarely occur.
			copy(ba[l1+diff:], ba[l1:])
		}

		// ignore return value. EncodeLength is writing in the middle of the array.
		_ = EncodeVarintZeroExtend(ba[l0:l0], uint64(length), varLength)
	}

	return ba, nil
}
