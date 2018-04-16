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
	"math"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
)

func makeField(fd *descriptor.FieldDescriptorProto) *field {
	packed := fd.IsPacked() || fd.IsPacked3()
	wireType := uint64(fd.WireType())
	fieldNumber := int(fd.GetNumber())
	if packed {
		wireType = uint64(proto.WireBytes)
	}

	return &field{
		protoKey: protoKey(fieldNumber, wireType),
		number:   fieldNumber,
		name:     fd.GetName(),
	}
}

func EncodePrimitive(v interface{}, etype descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
	switch t := v.(type) {
	case string:
		if etype != descriptor.FieldDescriptorProto_TYPE_STRING {
			return nil, fmt.Errorf("incorrect type:string, %v want:%s", v, etype)
		}
		ba, _ = EncodeVarint(ba, uint64(len(t)))
		ba = append(ba, t...)
	case bool:
		if etype != descriptor.FieldDescriptorProto_TYPE_BOOL {
			return nil, fmt.Errorf("incorrect type:bool, want:%s", etype)
		}
		// varint of 0 is 0, 1 is 1
		v := byte(0x0)
		if t {
			v = byte(0x1)
		}
		ba = append(ba, v)
	case int, int32, int64:
		if !isIntegerType(etype) {
			return nil, fmt.Errorf("incorrect type:%T, want:%s", v, etype)
		}
		vv, ok := Int64(t)
		if !ok {
			return nil, fmt.Errorf("incorrect type:%T, want:%s", v, etype)
		}
		ba, _ = EncodeVarint(ba, uint64(vv))
	case float64:
		switch etype {
		case descriptor.FieldDescriptorProto_TYPE_FLOAT:
			ba = EncodeFixed32(ba, uint64(math.Float32bits(float32(t))))
		case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
			ba = EncodeFixed64(ba, math.Float64bits(t))
		default:
			return nil, fmt.Errorf("incorrect type:float64, %v want:%s", v, etype)
		}
	default:
		return nil, fmt.Errorf("unknown type %v: %T", v, v)
	}

	return ba, nil
}

// EncodeString encode given string.
func EncodeString(v interface{}, ba []byte) ([]byte, error) {
	switch t := v.(type) {
	case string:
		ba, _ = EncodeVarint(ba, uint64(len(t)))
		ba = append(ba, t...)
	default:
		return nil, fmt.Errorf("incorrect type:%T, %v want:string", v, v)
	}
	return ba, nil
}

func EncodePrimitive2(v interface{}, etype descriptor.FieldDescriptorProto_Type, ba []byte) ([]byte, error) {
	switch t := v.(type) {
	case string:
		if etype != descriptor.FieldDescriptorProto_TYPE_STRING {
			return nil, fmt.Errorf("incorrect type:string, want:%s", etype)
		}
		ba, _ = EncodeVarint(ba, uint64(len(t)))
		ba = append(ba, t...)
	case bool:
		if etype != descriptor.FieldDescriptorProto_TYPE_BOOL {
			return nil, fmt.Errorf("incorrect type:bool, want:%s", etype)
		}
		// varint of 0 is 0, 1 is 1
		v := byte(0x0)
		if t {
			v = byte(0x1)
		}
		ba = append(ba, v)
	case int, int32, int64:
		if !isIntegerType(etype) {
			return nil, fmt.Errorf("incorrect type:%T, want:%s", v, etype)
		}
		vv, ok := Int64(t)
		if !ok {
			return nil, fmt.Errorf("incorrect type:%T, want:%s", v, etype)
		}
		ba, _ = EncodeVarint(ba, uint64(vv))
	case float64:
		switch etype {
		case descriptor.FieldDescriptorProto_TYPE_FLOAT:
			ba = EncodeFixed32(ba, uint64(math.Float32bits(float32(t))))
		case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
			ba = EncodeFixed64(ba, math.Float64bits(t))
		default:
			return nil, fmt.Errorf("incorrect type:float64, want:%s", etype)
		}
	default:
		return nil, fmt.Errorf("unknown type %v: %T", v, v)
	}

	return ba, nil
}

// EncodeVarintZeroExtend encodes x as Varint in ba. Ensures that encoding is at least
// minBytes long.
func EncodeVarintZeroExtend(ba []byte, x uint64, minBytes int) []byte {
	bn := 0
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

func EncodeFixed64(buf []byte, x uint64) []byte {
	buf = append(buf,
		uint8(x),
		uint8(x>>8),
		uint8(x>>16),
		uint8(x>>24),
		uint8(x>>32),
		uint8(x>>40),
		uint8(x>>48),
		uint8(x>>56))
	return buf
}

func EncodeFixed32(buf []byte, x uint64) []byte {
	buf = append(buf,
		uint8(x),
		uint8(x>>8),
		uint8(x>>16),
		uint8(x>>24))
	return buf
}

// protoKey returns the key which is comprised of field number and wire type.
func protoKey(fieldNumber int, wireType uint64) []byte {
	return proto.EncodeVarint((uint64(fieldNumber) << 3) | wireType)
}

func isIntegerType(etype descriptor.FieldDescriptorProto_Type) bool {
	switch etype {
	case descriptor.FieldDescriptorProto_TYPE_INT32,
		descriptor.FieldDescriptorProto_TYPE_UINT32,
		descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_UINT64:
		return true
	}

	return false
}

func Int64(v interface{}) (int64, bool) {
	switch c := v.(type) {
	case int:
		return int64(c), true
	case int8:
		return int64(c), true
	case int16:
		return int64(c), true
	case int32:
		return int64(c), true
	case int64:
		return int64(c), true
	default:
		return 0, false
	}
}
