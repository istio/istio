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
	"math"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"

	"istio.io/istio/mixer/pkg/protobuf/yaml"
)

func makeField(fd *descriptor.FieldDescriptorProto) *fieldEncoder {
	packed := fd.IsPacked() || fd.IsPacked3()
	wireType := uint64(fd.WireType())
	fieldNumber := int(fd.GetNumber())
	if packed {
		wireType = uint64(proto.WireBytes)
	}
	return &fieldEncoder{
		protoKey: protoKey(fieldNumber, wireType),
		number:   fieldNumber,
		name:     fd.GetName(),
		packed:   packed,
	}
}

// EncodeVarintZeroExtend encodes x as Varint in ba. Ensures that encoding is at least
// minBytes long.
func EncodeVarintZeroExtend(ba []byte, x uint64, minBytes int) []byte {
	var bn int
	ba, bn = EncodeVarint(ba, x)
	diff := minBytes - bn

	if diff <= 0 {
		return ba
	}

	ba[len(ba)-1] = 0x80 | ba[len(ba)-1]

	for ; diff > 1; diff-- {
		ba = append(ba, 0x80)
	}

	// must end with 0x00
	ba = append(ba, 0x00)

	return ba
}

// The low level encoding functions are based on
// https://github.com/gogo/protobuf/blob/master/proto/encode.go#Buffer

// EncodeVarint -- encodeVarint no allocations
func EncodeVarint(buf []byte, x uint64) ([]byte, int) {
	ol := len(buf)
	for x > 127 {
		buf = append(buf, 0x80|uint8(x&0x7F))
		x >>= 7
	}
	buf = append(buf, uint8(x))
	return buf, len(buf) - ol
}

func encodeFixed64(buf []byte, x uint64) []byte {
	return append(buf,
		uint8(x),
		uint8(x>>8),
		uint8(x>>16),
		uint8(x>>24),
		uint8(x>>32),
		uint8(x>>40),
		uint8(x>>48),
		uint8(x>>56))
}

func encodeFixed32(buf []byte, x uint64) []byte {
	return append(buf,
		uint8(x),
		uint8(x>>8),
		uint8(x>>16),
		uint8(x>>24))
}

func encodeZigzag32(buf []byte, x uint64) []byte {
	// use signed number to get arithmetic right shift.
	buf, _ = EncodeVarint(buf, uint64((uint32(x)<<1)^uint32((int32(x)>>31))))
	return buf
}

func encodeZigzag64(buf []byte, x uint64) []byte {
	// use signed number to get arithmetic right shift.
	buf, _ = EncodeVarint(buf, (x<<1)^uint64((int64(x)>>63)))
	return buf
}

// protoKey returns the key which is comprised of fieldEncoder number and wire type.
func protoKey(fieldNumber int, wireType uint64) []byte {
	buf := make([]byte, 0, 2) // this is enough to encode protokey
	buf, _ = EncodeVarint(buf, (uint64(fieldNumber)<<3)|wireType)
	return buf
}

func badTypeError(v interface{}, wantType string) error {
	return fmt.Errorf("badTypeError: value: '%v' is of type:%T, want:%s", v, v, wantType)
}

// Low level Encode Funcs

// EncodeDouble encode Double.
func EncodeDouble(v interface{}, ba []byte) ([]byte, error) {
	c, ok := yaml.ToFloat(v)
	if !ok {
		return nil, badTypeError(v, "double")
	}
	return encodeFixed64(ba, math.Float64bits(c)), nil
}

// EncodeFloat encode Float.
func EncodeFloat(v interface{}, ba []byte) ([]byte, error) {
	c, ok := yaml.ToFloat(v)
	if !ok {
		return nil, badTypeError(v, "float64")
	}
	return encodeFixed32(ba, uint64(math.Float32bits(float32(c)))), nil
}

// EncodeInt encodes (U)INT(32/64)
func EncodeInt(v interface{}, ba []byte) ([]byte, error) {
	c, ok := yaml.ToInt64(v)
	if !ok {
		return nil, badTypeError(v, "int")
	}
	return encodeInt(c, ba)
}

func encodeInt(v int64, ba []byte) ([]byte, error) {
	ba, _ = EncodeVarint(ba, uint64(v))
	return ba, nil
}

// EncodeFixed64 encodes FIXED64, SFIXED64
func EncodeFixed64(v interface{}, ba []byte) ([]byte, error) {
	c, ok := yaml.ToInt64(v)
	if !ok {
		return nil, badTypeError(v, "int")
	}

	return encodeFixed64(ba, uint64(c)), nil
}

// EncodeFixed32 encodes FIXED32, SFIXED32
func EncodeFixed32(v interface{}, ba []byte) ([]byte, error) {
	c, ok := yaml.ToInt64(v)
	if !ok {
		return nil, badTypeError(v, "int")
	}

	return encodeFixed32(ba, uint64(c)), nil
}

// EncodeBool encodes input as boolean.
func EncodeBool(v interface{}, ba []byte) ([]byte, error) {
	c, ok := v.(bool)
	if !ok {
		return nil, badTypeError(v, "bool")
	}
	return encodeBool(c, ba), nil
}

const trueByte = byte(0x1)
const falseByte = byte(0x0)

func encodeBool(c bool, ba []byte) []byte {
	if c {
		ba = append(ba, trueByte)
	} else {
		ba = append(ba, falseByte)
	}
	return ba
}

// EncodeString encodes string
func EncodeString(v interface{}, ba []byte) ([]byte, error) {
	t, ok := v.(string)
	if !ok {
		return nil, badTypeError(v, "string")
	}
	return encodeString(t, ba)
}

func encodeString(s string, ba []byte) ([]byte, error) {
	ba, _ = EncodeVarint(ba, uint64(len(s)))
	ba = append(ba, s...)
	return ba, nil
}

// EncodeSInt32 encodes sint32 as zigzag
func EncodeSInt32(v interface{}, ba []byte) ([]byte, error) {
	c, ok := yaml.ToInt64(v)
	if !ok {
		return nil, badTypeError(v, "int")
	}
	return encodeZigzag32(ba, uint64(c)), nil
}

// EncodeSInt64 encodes sint64 as zigzag
func EncodeSInt64(v interface{}, ba []byte) ([]byte, error) {
	c, ok := yaml.ToInt64(v)
	if !ok {
		return nil, badTypeError(v, "int")
	}
	return encodeZigzag64(ba, uint64(c)), nil
}

// EncodeEnum encodes enum from string or int inputs
func EncodeEnum(v interface{}, ba []byte, enumValues []*descriptor.EnumValueDescriptorProto) ([]byte, error) {
	if vs, ok := v.(string); ok {
		return encodeEnumString(vs, ba, enumValues)
	}

	if vi, ok := yaml.ToInt64(v); ok {
		return encodeEnumInt(vi, ba, enumValues)
	}

	return nil, fmt.Errorf("unable to encode enum %v of type: %T", v, v)
}

func encodeEnumInt(v int64, ba []byte, enumValues []*descriptor.EnumValueDescriptorProto) ([]byte, error) {
	for _, val := range enumValues {
		if val.GetNumber() == int32(v) {
			ba, _ = EncodeVarint(ba, uint64(val.GetNumber()))
			return ba, nil
		}
	}
	return nil, fmt.Errorf("unknown value: %v, enum:%v", v, enumValues)
}

func encodeEnumString(v string, ba []byte, enumValues []*descriptor.EnumValueDescriptorProto) ([]byte, error) {
	for _, val := range enumValues {
		if val.GetName() == v {
			ba, _ = EncodeVarint(ba, uint64(val.GetNumber()))
			return ba, nil
		}
	}
	return nil, fmt.Errorf("unknown value: %v, enum:%v", v, enumValues)
}

// transformQuotedString removes quotes from strings and returns true
// if quotes were removed. constant strings are not expressions.
func transformQuotedString(v interface{}) (interface{}, bool) {
	var ok bool
	var s string
	if s, ok = v.(string); !ok {
		return v, false
	}

	if len(s) < 2 {
		return v, false
	}

	if (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) ||
		(strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) {
		return s[1 : len(s)-1], true
	}
	return s, false
}
