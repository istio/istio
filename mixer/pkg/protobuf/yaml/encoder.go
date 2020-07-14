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

package yaml

import (
	"fmt"
	"io/ioutil"
	"math"
	"sort"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
)

type (
	namedEncoderFn func(data interface{}) ([]byte, error)

	// Encoder transforms yaml that represents protobuf data into []byte
	Encoder struct {
		resolver     Resolver
		namedEncoder map[string]namedEncoderFn
	}
)

// NewEncoder creates an Encoder
func NewEncoder(fds *descriptor.FileDescriptorSet) *Encoder {
	resolver := NewResolver(fds)
	return &Encoder{resolver: resolver, namedEncoder: namedEncoderRegistry}
}

// EncodeBytes creates []byte from a yaml representation of a proto.
func (e *Encoder) EncodeBytes(data map[string]interface{}, msgName string, skipUnknown bool) ([]byte, error) {
	buf := GetBuffer()
	defer func() { PutBuffer(buf) }()

	if err := e.coreEncodeBytes(buf, data, msgName, skipUnknown); err != nil {
		return nil, err
	}

	original := buf.Bytes()
	result := make([]byte, len(original))
	copy(result, original)
	return result, nil
}

// encodeBytes updates a proto.Buffer from a yaml representation of a proto and outputs into a buffer.
func (e *Encoder) encodeBytes(data map[string]interface{}, msgName string, skipUnknown bool, dest *proto.Buffer) error {
	buf := GetBuffer()
	defer func() { PutBuffer(buf) }()

	if err := e.coreEncodeBytes(buf, data, msgName, skipUnknown); err != nil {
		return err
	}

	_ = dest.EncodeRawBytes(buf.Bytes())
	return nil
}

// coreEncodeBytes updates a proto.Buffer from a yaml representation of a proto.
func (e *Encoder) coreEncodeBytes(buf *proto.Buffer, data map[string]interface{}, msgName string, skipUnknown bool) error {
	message := e.resolver.ResolveMessage(msgName)
	if message == nil {
		return fmt.Errorf("cannot resolve message '%s'", msgName)
	}
	// Sort entries so the result is deterministic.
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := data[k]
		fd := FindFieldByName(message, k)
		if fd == nil {
			if skipUnknown {
				continue
			}
			return fmt.Errorf("field '%s' not found in message '%s'", k, message.GetName())
		}

		if err := e.visit(k, v, fd, skipUnknown, buf); err != nil {
			return err
		}
	}

	return nil
}

func (e *Encoder) visit(name string, data interface{}, field *descriptor.FieldDescriptorProto, skipUnknown bool, buffer *proto.Buffer) error {
	if data == nil {
		return nil
	}
	repeated := field.IsRepeated()
	packed := field.IsPacked() || field.IsPacked3()
	wireType := uint64(field.WireType())
	fieldNumber := int(field.GetNumber())
	if packed {
		// packed fields are encoded as bytes over the wire, irrespective of field's wire format.
		wireType = uint64(proto.WireBytes)
	}
	switch field.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		if packed {
			//	generated proto code for repeated packed double fields
			//
			// 	dAtA[i] = 0xba
			// 	i++
			// 	dAtA[i] = 0x1
			// 	i++
			// 	i = encodeVarintTypes(dAtA, i, uint64(len(m.RDbl)*8))
			// 	for _, num := range m.RDbl {
			// 		f4 := math.Float64bits(float64(num))
			//		encoding_binary.LittleEndian.PutUint64(dAtA[i:], uint64(f4))
			//		i += 8
			// 	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]float64", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeVarint(uint64(len(v) * 8))
			for i, iface := range v {
				c, ok := ToFloat(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "float64", iface)
				}
				_ = buffer.EncodeFixed64(math.Float64bits(c))
			}
		} else if repeated {
			//	generated proto code for repeated unpacked double fields
			//
			//	for _, num := range m.RDblUnpacked {
			//		dAtA[i] = 0xc1
			// 		i++
			//		dAtA[i] = 0x1
			//		i++
			//		f5 := math.Float64bits(float64(num))
			//		encoding_binary.LittleEndian.PutUint64(dAtA[i:], uint64(f5))
			//		i += 8
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]float64", data)
			}

			for i, iface := range v {
				c, ok := ToFloat(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "float64", iface)
				}
				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
				_ = buffer.EncodeFixed64(math.Float64bits(c))
			}
		} else {
			//	generated proto code for double fields
			//
			//	dAtA[i] = 0x11
			//	i++
			//	encoding_binary.LittleEndian.PutUint64(dAtA[i:], uint64(math.Float64bits(float64(m.Dbl))))
			//	i += 8
			v, ok := ToFloat(data)
			if !ok {
				return badTypeError(name, "float64", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeFixed64(math.Float64bits(v))
		}

	case descriptor.FieldDescriptorProto_TYPE_FLOAT:
		if packed {
			// 	generated proto code for repeated packed float fields
			//
			//	dAtA[i] = 0xf2
			//	i++
			//	dAtA[i] = 0x1
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(len(m.RFlt)*4))
			//	for _, num := range m.RFlt {
			//		f8 := math.Float32bits(float32(num))
			//		encoding_binary.LittleEndian.PutUint32(dAtA[i:], uint32(f8))
			//		i += 4
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]float32", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeVarint(uint64(len(v) * 4))
			for i, iface := range v {
				c, ok := ToFloat(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "float32", iface)
				}
				_ = buffer.EncodeFixed32(uint64(math.Float32bits(float32(c))))
			}
		} else if repeated {
			// 	generated proto code for repeated unpacked float fields
			//
			//	for _, num := range m.RFltUnpacked {
			//		dAtA[i] = 0xfd
			//		i++
			//		dAtA[i] = 0x1
			//		i++
			//		f9 := math.Float32bits(float32(num))
			//		encoding_binary.LittleEndian.PutUint32(dAtA[i:], uint32(f9))
			//		i += 4
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]float32", data)
			}

			for i, iface := range v {
				c, ok := ToFloat(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "float32", iface)
				}
				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
				_ = buffer.EncodeFixed32(uint64(math.Float32bits(float32(c))))
			}
		} else {
			//	generated proto code for float fields
			//
			//	dAtA[i] = 0xed
			//	i++
			//	dAtA[i] = 0x1
			//	i++
			//	encoding_binary.LittleEndian.PutUint32(dAtA[i:], uint32(math.Float32bits(float32(m.Flt))))
			//	i += 4
			v, ok := ToFloat(data)
			if !ok {
				return badTypeError(name, "float32", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeFixed32(uint64(math.Float32bits(float32(v))))
		}
	case descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_UINT64,
		descriptor.FieldDescriptorProto_TYPE_INT32,
		descriptor.FieldDescriptorProto_TYPE_UINT32:
		if packed {
			//	generated proto code for repeated packed int32/64/uint32/64 fields
			//
			//	dAtA11 := make([]byte, len(m.RI64)*10)
			//	var j10 int
			//	for _, num1 := range m.RI64 {
			//		num := uint64(num1)
			//		for num >= 1<<7 {
			//			dAtA11[j10] = uint8(uint64(num)&0x7f | 0x80)
			//			num >>= 7
			//			j10++
			//		}
			//		dAtA11[j10] = uint8(num)
			//		j10++
			//	}
			//	dAtA[i] = 0x8a
			//	i++
			//	dAtA[i] = 0x2
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(j10))
			//	i += copy(dAtA[i:], dAtA11[:j10])
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]int", data)
			}

			tmpBuffer := GetBuffer()
			for i, iface := range v {
				c, ok := ToInt64(iface)
				if !ok {
					PutBuffer(tmpBuffer)
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "int", iface)
				}
				_ = tmpBuffer.EncodeVarint(uint64(c))
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeRawBytes(tmpBuffer.Bytes())
			PutBuffer(tmpBuffer)
		} else if repeated {
			// 	generated proto code for repeated unpacked int32/64/uint32/64 fields
			//
			//	for _, num := range m.RI64Unpacked {
			//		dAtA[i] = 0x90
			//		i++
			//		dAtA[i] = 0x2
			//		i++
			//		i = encodeVarintTypes(dAtA, i, uint64(num))
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]int", data)
			}

			for i, iface := range v {
				c, ok := ToInt64(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "int", iface)
				}
				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
				_ = buffer.EncodeVarint(uint64(c))
			}
		} else {
			// 	generated proto code for int32/64/uint32/64 fields
			//
			//	dAtA[i] = 0x18
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(m.I64))
			v, ok := ToInt64(data)
			if !ok {
				return badTypeError(name, "int", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeVarint(uint64(v))
		}
	case descriptor.FieldDescriptorProto_TYPE_FIXED64,
		descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		if packed {
			// 	generated proto code for repeated packed fixed64/sfixed64 fields
			//
			//	dAtA[i] = 0xea
			//	i++
			//	dAtA[i] = 0x2
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(len(m.RF64)*8))
			//	for _, num := range m.RF64 {
			//		encoding_binary.LittleEndian.PutUint64(dAtA[i:], uint64(num))
			//		i += 8
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]int", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeVarint(uint64(len(v) * 8))
			for i, iface := range v {
				c, ok := ToInt64(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "int", iface)
				}
				_ = buffer.EncodeFixed64(uint64(c))
			}
		} else if repeated {
			//	generated proto code for repeated unpacked fixed64/sfixed64 fields
			//
			//	for _, num := range m.RF64Unpacked {
			//		dAtA[i] = 0xf1
			//		i++
			//		dAtA[i] = 0x2
			//		i++
			//		encoding_binary.LittleEndian.PutUint64(dAtA[i:], uint64(num))
			//		i += 8
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]int", data)
			}

			for i, iface := range v {
				c, ok := ToInt64(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "int", iface)
				}
				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
				_ = buffer.EncodeFixed64(uint64(c))
			}
		} else {
			// 	generated proto code for fixed64/sfixed64 fields
			//
			//	dAtA[i] = 0xe1
			//	i++
			//	dAtA[i] = 0x2
			//	i++
			//	encoding_binary.LittleEndian.PutUint64(dAtA[i:], uint64(m.F64))
			//	i += 8
			v, ok := ToInt64(data)
			if !ok {
				return badTypeError(name, "int", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeFixed64(uint64(v))
		}
	case descriptor.FieldDescriptorProto_TYPE_FIXED32,
		descriptor.FieldDescriptorProto_TYPE_SFIXED32:
		if packed {
			// 	generated proto code for repeated packed fixed32/sfixed32 fields
			//
			//	dAtA[i] = 0x9a
			//	i++
			//	dAtA[i] = 0x3
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(len(m.RF32)*4))
			//	for _, num := range m.RF32 {
			//		encoding_binary.LittleEndian.PutUint32(dAtA[i:], uint32(num))
			//		i += 4
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]int", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeVarint(uint64(len(v) * 4))
			for i, iface := range v {
				c, ok := ToInt64(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "int", iface)
				}
				_ = buffer.EncodeFixed32(uint64(c))
			}
		} else if repeated {
			// 	generated proto code for repeated unpacked fixed32/sfixed32 fields
			//
			//	for _, num := range m.RF32Unpacked {
			//		dAtA[i] = 0xa5
			//		i++
			//		dAtA[i] = 0x3
			//		i++
			//		encoding_binary.LittleEndian.PutUint32(dAtA[i:], uint32(num))
			//		i += 4
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]int", data)
			}

			for i, iface := range v {
				c, ok := ToInt64(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "int", iface)
				}
				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
				_ = buffer.EncodeFixed32(uint64(c))
			}
		} else {
			// 	generated proto code for fixed32/sfixed32 fields
			//
			//	dAtA[i] = 0x95
			//	i++
			//	dAtA[i] = 0x3
			//	i++
			//	encoding_binary.LittleEndian.PutUint32(dAtA[i:], uint32(m.F32))
			//	i += 4
			v, ok := ToInt64(data)
			if !ok {
				return badTypeError(name, "int", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeFixed32(uint64(v))
		}
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		if packed {
			// 	generated proto code for repeated packed bool fields
			//
			//	dAtA[i] = 0xc2
			//	i++
			//	dAtA[i] = 0x3
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(len(m.RB)))
			//	for _, b := range m.RB {
			//		if b {
			//			dAtA[i] = 1
			//		} else {
			//			dAtA[i] = 0
			//		}
			//		i++
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]bool", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeVarint(uint64(len(v)))
			for i, iface := range v {
				c, ok := iface.(bool)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "bool", iface)
				}
				val := uint64(0)
				if c {
					val = uint64(1)
				}
				_ = buffer.EncodeVarint(val)
			}
		} else if repeated {
			// 	generated proto code for repeated unpacked bool fields
			//
			//	for _, b := range m.RBUnpacked {
			//		dAtA[i] = 0xc8
			//		i++
			//		dAtA[i] = 0x3
			//		i++
			//		if b {
			//			dAtA[i] = 1
			//		} else {
			//			dAtA[i] = 0
			//		}
			//		i++
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]bool", data)
			}

			for i, iface := range v {
				c, ok := iface.(bool)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "bool", iface)
				}
				val := uint64(0)
				if c {
					val = uint64(1)
				}
				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
				_ = buffer.EncodeVarint(val)
			}
		} else {
			// 	generated proto code for bool fields
			//
			//	dAtA[i] = 0x20
			//	i++
			//	if m.B {
			//		dAtA[i] = 1
			//	} else {
			//		dAtA[i] = 0
			//	}
			//	i++
			v, ok := data.(bool)
			if !ok {
				return badTypeError(name, "bool", data)
			}
			val := uint64(0)
			if v {
				val = uint64(1)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeVarint(val)
		}
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		if repeated {
			// 	generated proto code for repeated string fields
			//
			//	for _, s := range m.RStr {
			//		dAtA[i] = 0xd2
			//		i++
			//		dAtA[i] = 0x3
			//		i++
			//		l = len(s)
			//		for l >= 1<<7 {
			//			dAtA[i] = uint8(uint64(l)&0x7f | 0x80)
			//			l >>= 7
			//			i++
			//		}
			//		dAtA[i] = uint8(l)
			//		i++
			//		i += copy(dAtA[i:], s)
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]string", data)
			}

			for i, iface := range v {
				c, ok := iface.(string)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "string", iface)
				}
				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
				_ = buffer.EncodeStringBytes(c)
			}
		} else {
			// 	generated proto code for string fields
			//
			//	dAtA[i] = 0xa
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(len(m.Str)))
			//	i += copy(dAtA[i:], m.Str)
			v, ok := data.(string)
			if !ok {
				return badTypeError(name, "string", data)
			}

			// Errors from proto.Buffer.Encode* are always nil, therefore ignoring.
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeStringBytes(v)
		}
	case descriptor.FieldDescriptorProto_TYPE_SINT32:
		if packed {
			// 	generated proto code for repeated packed sint32 fields
			//
			//	dAtA18 := make([]byte, len(m.RSi32)*5)
			//	var j19 int
			//	for _, num := range m.RSi32 {
			//		x20 := (uint32(num) << 1) ^ uint32((num >> 31))
			//		for x20 >= 1<<7 {
			//			dAtA18[j19] = uint8(uint64(x20)&0x7f | 0x80)
			//			j19++
			//			x20 >>= 7
			//		}
			//		dAtA18[j19] = uint8(x20)
			//		j19++
			//	}
			//	dAtA[i] = 0xe2
			//	i++
			//	dAtA[i] = 0x3
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(j19))
			//	i += copy(dAtA[i:], dAtA18[:j19])
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]int", data)
			}

			tmpBuffer := GetBuffer()
			for i, iface := range v {
				c, ok := ToInt64(iface)
				if !ok {
					PutBuffer(tmpBuffer)
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "int", iface)
				}
				_ = tmpBuffer.EncodeZigzag32(uint64(c))
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeRawBytes(tmpBuffer.Bytes())
			PutBuffer(tmpBuffer)

		} else if repeated {
			// 	generated proto code for repeated unpacked sint32 fields
			//
			//	for _, num := range m.RSi32Unpacked {
			//		dAtA[i] = 0xe8
			//		i++
			//		dAtA[i] = 0x3
			//		i++
			//		x21 := (uint32(num) << 1) ^ uint32((num >> 31))
			//		for x21 >= 1<<7 {
			//			dAtA[i] = uint8(uint64(x21)&0x7f | 0x80)
			//			x21 >>= 7
			//			i++
			//		}
			//		dAtA[i] = uint8(x21)
			//		i++
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]int", data)
			}

			for i, iface := range v {
				c, ok := ToInt64(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "int", iface)
				}
				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
				_ = buffer.EncodeZigzag32(uint64(c))
			}
		} else {
			// 	generated proto code for sint32 fields
			//
			//	dAtA[i] = 0xd8
			//	i++
			//	dAtA[i] = 0x3
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64((uint32(m.Si32)<<1)^uint32((m.Si32>>31))))
			v, ok := ToInt64(data)
			if !ok {
				return badTypeError(name, "int", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeZigzag32(uint64(v))
		}
	case descriptor.FieldDescriptorProto_TYPE_SINT64:
		if packed {
			// 	generated proto code for repeated packed sint64 fields
			//
			//	var j22 int
			//	dAtA24 := make([]byte, len(m.RSi64)*10)
			//	for _, num := range m.RSi64 {
			//		x23 := (uint64(num) << 1) ^ uint64((num >> 63))
			//		for x23 >= 1<<7 {
			//			dAtA24[j22] = uint8(uint64(x23)&0x7f | 0x80)
			//			j22++
			//			x23 >>= 7
			//		}
			//		dAtA24[j22] = uint8(x23)
			//		j22++
			//	}
			//	dAtA[i] = 0xfa
			//	i++
			//	dAtA[i] = 0x3
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(j22))
			//	i += copy(dAtA[i:], dAtA24[:j22])
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]int", data)
			}

			tmpBuffer := GetBuffer()
			for i, iface := range v {
				c, ok := ToInt64(iface)
				if !ok {
					PutBuffer(tmpBuffer)
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "int", iface)
				}
				_ = tmpBuffer.EncodeZigzag64(uint64(c))
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeRawBytes(tmpBuffer.Bytes())
			PutBuffer(tmpBuffer)
		} else if repeated {
			// 	generated proto code for repeated unpacked sint64 fields
			//
			//	for _, num := range m.RSi64Unpacked {
			//		dAtA[i] = 0x80
			//		i++
			//		dAtA[i] = 0x4
			//		i++
			//		x25 := (uint64(num) << 1) ^ uint64((num >> 63))
			//		for x25 >= 1<<7 {
			//			dAtA[i] = uint8(uint64(x25)&0x7f | 0x80)
			//			x25 >>= 7
			//			i++
			//		}
			//		dAtA[i] = uint8(x25)
			//		i++
			//	}
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, "[]int", data)
			}

			for i, iface := range v {
				c, ok := ToInt64(iface)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), "int", iface)
				}
				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
				_ = buffer.EncodeZigzag64(uint64(c))
			}
		} else {
			// 	generated proto code for sint64 fields
			//
			//	dAtA[i] = 0xf0
			//	i++
			//	dAtA[i] = 0x3
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64((uint64(m.Si64)<<1)^uint64((m.Si64>>63))))
			v, ok := ToInt64(data)
			if !ok {
				return badTypeError(name, "int", data)
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeZigzag64(uint64(v))
		}

	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		if isMap(e.resolver, field) {
			// 	generated proto code for map fields
			//
			//	if len(m.MapStrStr) > 0 {
			//		for k, v := range m.MapStrStr {
			//			_ = k
			//			_ = v
			//			mapEntrySize := 1 + len(k) + sovTypes(uint64(len(k))) + 1 + len(v) + sovTypes(uint64(len(v)))
			//			n += mapEntrySize + 2 + sovTypes(uint64(mapEntrySize))
			//		}
			//	}
			//	if len(m.MapStrMsg) > 0 {
			//		for k, v := range m.MapStrMsg {
			//			_ = k
			//			_ = v
			//			l = 0
			//			if v != nil {
			//				l = v.Size()
			//				l += 1 + sovTypes(uint64(l))
			//			}
			//			mapEntrySize := 1 + len(k) + sovTypes(uint64(len(k))) + l
			//			n += mapEntrySize + 2 + sovTypes(uint64(mapEntrySize))
			//		}
			//	}
			//	if len(m.MapI32Msg) > 0 {
			//		for k, v := range m.MapI32Msg {
			//			_ = k
			//			_ = v
			//			l = 0
			//			if v != nil {
			//				l = v.Size()
			//				l += 1 + sovTypes(uint64(l))
			//			}
			//			mapEntrySize := 1 + sovTypes(uint64(k)) + l
			//			n += mapEntrySize + 2 + sovTypes(uint64(mapEntrySize))
			//		}
			//	}
			//	if len(m.MapStrEnum) > 0 {
			//		for k, v := range m.MapStrEnum {
			//			_ = k
			//			_ = v
			//			mapEntrySize := 1 + len(k) + sovTypes(uint64(len(k))) + 1 + sovTypes(uint64(v))
			//			n += mapEntrySize + 2 + sovTypes(uint64(mapEntrySize))
			//		}
			//	}
			mapInfo := e.mapType(field)
			keyType := typeName(mapInfo.KeyField)
			valType := typeName(mapInfo.ValueField)
			v, ok := data.(map[string]interface{})
			if !ok {
				return badTypeError(name, fmt.Sprintf("map<%s, %s>", keyType, valType), data)
			}

			//	Maps always have a wire format like this:
			// 	message MapEntry {
			//		key_type key = 1;
			//		value_type value = 2;
			//	}
			//	repeated MapEntry map = N;

			// Sort entries so that encoded bytes are deterministic
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			// So, we can create a map[key_type]value_type and use it to encode the MapEntry, repeatedly.
			for _, key := range keys {
				val := v[key]
				tmpMapEntry := make(map[string]interface{}, 2)
				tmpMapEntry["key"] = key
				tmpMapEntry["value"] = val

				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))

				if err := e.encodeBytes(tmpMapEntry, field.GetTypeName(), skipUnknown, buffer); err != nil {
					return fmt.Errorf("/%s: '%v'", fmt.Sprintf("%s[%v]", name, key), err)
				}
			}
		} else if repeated {
			// 	generated proto code for repeated message fields
			//
			//	for _, msg := range m.ROth {
			//		dAtA[i] = 0xd2
			//		i++
			//		dAtA[i] = 0x1
			//		i++
			//		i = encodeVarintTypes(dAtA, i, uint64(msg.Size()))
			//		n, err := msg.MarshalTo(dAtA[i:])
			//		if err != nil {
			//			return 0, err
			//		}
			//		i += n
			//	}
			tName := strings.TrimPrefix(field.GetTypeName(), ".")
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, fmt.Sprintf("[]%s", tName), data)
			}

			for i, iface := range v {
				c, ok := iface.(map[string]interface{})
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), tName, iface)
				}

				_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
				if err := e.encodeBytes(c, field.GetTypeName(), skipUnknown, buffer); err != nil {
					return fmt.Errorf("/%s: '%v'", fmt.Sprintf("%s[%d]", name, i), err)
				}
			}
		} else {
			// 	generated proto code for field of message type
			//
			//	dAtA[i] = 0x5a
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(m.Oth.Size()))
			//	n1, err := m.Oth.MarshalTo(dAtA[i:])
			//	if err != nil {
			//		return 0, err
			//	}
			//	i += n1
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))

			ne := e.namedEncoder[field.GetTypeName()]
			if ne != nil {
				bytes, err := ne(data)
				if err != nil {
					return fmt.Errorf("/%s: '%v'", name, err)
				}
				_ = buffer.EncodeRawBytes(bytes)
			} else {
				v, ok := data.(map[string]interface{})
				if !ok {
					return badTypeError(name, strings.TrimPrefix(field.GetTypeName(), "."), data)
				}

				if err := e.encodeBytes(v, field.GetTypeName(), skipUnknown, buffer); err != nil {
					return fmt.Errorf("/%s: '%v'", name, err)
				}
			}

			return nil
		}

	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		if packed {
			// 	generated proto code for repeated packed enum fields
			//
			//	dAtA7 := make([]byte, len(m.REnm)*10)
			//	var j6 int
			//	for _, num := range m.REnm {
			//		for num >= 1<<7 {
			//			dAtA7[j6] = uint8(uint64(num)&0x7f | 0x80)
			//			num >>= 7
			//			j6++
			//		}
			//		dAtA7[j6] = uint8(num)
			//		j6++
			//	}
			//	dAtA[i] = 0xe2
			//	i++
			//	dAtA[i] = 0x1
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(j6))
			//	i += copy(dAtA[i:], dAtA7[:j6])
			enumName := fmt.Sprintf("enum(%s)", strings.TrimPrefix(field.GetTypeName(), "."))
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, fmt.Sprintf("[]%s", enumName), data)
			}

			tmpBuffer := GetBuffer()
			for i, iface := range v {
				enumValStr, ok := iface.(string)
				if !ok {
					PutBuffer(tmpBuffer)
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), enumName, iface)
				}
				enum := e.resolver.ResolveEnum(field.GetTypeName()) // enum must exist since resolver has full transitive closure.
				valMatched := false
				for _, val := range enum.Value {
					if val.GetName() == enumValStr {
						_ = tmpBuffer.EncodeVarint(uint64(val.GetNumber()))
						valMatched = true
						break
					}
				}
				if !valMatched {
					PutBuffer(tmpBuffer)
					return fmt.Errorf("unrecognized enum value '%s' for enum '%s'", enumValStr, enum.GetName())
				}
			}
			_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
			_ = buffer.EncodeRawBytes(tmpBuffer.Bytes())
			PutBuffer(tmpBuffer)
		} else if repeated {
			// 	generated proto code for repeated unpacked enum fields
			//
			//	for _, num := range m.REnmUnpacked {
			//		dAtA[i] = 0xc8
			//		i++
			//		dAtA[i] = 0x11
			//		i++
			//		i = encodeVarintTypes(dAtA, i, uint64(num))
			//	}
			enumName := fmt.Sprintf("enum(%s)", strings.TrimPrefix(field.GetTypeName(), "."))
			v, ok := data.([]interface{})
			if !ok {
				return badTypeError(name, fmt.Sprintf("[]%s", enumName), data)
			}

			for i, iface := range v {
				enumValStr, ok := iface.(string)
				if !ok {
					return badTypeError(fmt.Sprintf("%s[%d]", name, i), enumName, iface)
				}
				enum := e.resolver.ResolveEnum(field.GetTypeName()) // enum must exist since resolver has full transitive closure.
				valMatched := false
				for _, val := range enum.Value {
					if val.GetName() == enumValStr {
						_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
						_ = buffer.EncodeVarint(uint64(val.GetNumber()))
						valMatched = true
						break
					}
				}
				if !valMatched {
					return fmt.Errorf("unrecognized enum value '%s' for enum '%s'", enumValStr, enum.GetName())
				}
			}
		} else {
			// 	generated proto code for enum fields
			//
			//	dAtA[i] = 0x68
			//	i++
			//	i = encodeVarintTypes(dAtA, i, uint64(m.Enm))
			enumValStr, ok := data.(string)
			if !ok {
				return badTypeError(name, fmt.Sprintf("enum(%s)", strings.TrimPrefix(field.GetTypeName(), ".")), data)
			}
			enum := e.resolver.ResolveEnum(field.GetTypeName()) // enum must exist since resolver has full transitive closure.
			for _, val := range enum.Value {
				if val.GetName() == enumValStr {
					_ = buffer.EncodeVarint(encodeIndexAndType(fieldNumber, wireType))
					_ = buffer.EncodeVarint(uint64(val.GetNumber()))
					return nil
				}
			}
			return fmt.Errorf("unrecognized enum value '%s' for enum '%s'", enumValStr, enum.GetName())
		}
	default:
		return fmt.Errorf("unrecognized field type '%s'", field.Type.String())
	}

	return nil
}

func badTypeError(name, wantType string, data interface{}) error {
	return fmt.Errorf("field '%s' is of type '%T' instead of expected type '%s'", name, data, wantType)
}

func encodeIndexAndType(index int, typeid uint64) uint64 {
	return (uint64(index) << 3) | typeid
}

func isMap(resolver Resolver, field *descriptor.FieldDescriptorProto) bool {
	desc := resolver.ResolveMessage(field.GetTypeName())
	if desc == nil || !desc.GetOptions().GetMapEntry() {
		return false
	}
	return true
}

type mapDescriptor struct {
	KeyField   *descriptor.FieldDescriptorProto
	ValueField *descriptor.FieldDescriptorProto
}

func (e *Encoder) mapType(field *descriptor.FieldDescriptorProto) *mapDescriptor {
	desc := e.resolver.ResolveMessage(field.GetTypeName()) // desc must exists, resolver has full transitive closure.

	m := &mapDescriptor{
		KeyField:   desc.Field[0],
		ValueField: desc.Field[1],
	}
	return m
}

// nolint: goconst
func typeName(field *descriptor.FieldDescriptorProto) string {
	typ := ""
	switch *field.Type {
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		typ = "float64"
	case descriptor.FieldDescriptorProto_TYPE_FLOAT:
		typ = "float32"
	case descriptor.FieldDescriptorProto_TYPE_INT64:
		typ = "int64"
	case descriptor.FieldDescriptorProto_TYPE_UINT64:
		typ = "uint64"
	case descriptor.FieldDescriptorProto_TYPE_INT32:
		typ = "int32"
	case descriptor.FieldDescriptorProto_TYPE_UINT32:
		typ = "uint32"
	case descriptor.FieldDescriptorProto_TYPE_FIXED64:
		typ = "uint64"
	case descriptor.FieldDescriptorProto_TYPE_FIXED32:
		typ = "uint32"
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		typ = "bool"
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		typ = "string"
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		typ = strings.Trim(field.GetTypeName(), ".")
	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		typ = strings.Trim(field.GetTypeName(), ".")
	case descriptor.FieldDescriptorProto_TYPE_SFIXED32:
		typ = "int32"
	case descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		typ = "int64"
	case descriptor.FieldDescriptorProto_TYPE_SINT32:
		typ = "int32"
	case descriptor.FieldDescriptorProto_TYPE_SINT64:
		typ = "int64"
	}
	return typ
}

// GetFileDescSet reads proto filedescriptor set from a given file.
func GetFileDescSet(path string) (*descriptor.FileDescriptorSet, error) {
	byts, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fds := &descriptor.FileDescriptorSet{}
	if err = proto.Unmarshal(byts, fds); err != nil {
		return nil, err
	}

	return fds, nil
}
