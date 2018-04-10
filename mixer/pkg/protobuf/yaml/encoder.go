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

package yaml

import (
	"fmt"
	"math"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
)

type (
	bufferWriter struct {
		resolver    Resolver
		buffer      *proto.Buffer
		walker      Walker
		skipUnknown bool
	}
	// Encoder transforms yaml that represents protobuf data into []byte
	Encoder struct {
		walker   Walker
		resolver Resolver
	}
)

// NewEncoder creates a Encoder
func NewEncoder(fds *descriptor.FileDescriptorSet) *Encoder {
	resolver := NewResolver(fds)
	return &Encoder{resolver: resolver, walker: NewWalker(resolver)}
}

// EncodeBytes creates []byte from a yaml representation of a proto.
func (c *Encoder) EncodeBytes(data map[interface{}]interface{}, msgName string, skipUnknown bool) ([]byte, error) {
	buf := GetBuffer()
	msgWriter := newBufferWriter(c.resolver, buf, c.walker, skipUnknown)
	err := c.walker.Walk(data, msgName, skipUnknown, msgWriter)
	bytes := buf.Bytes()
	PutBuffer(buf)

	if err != nil {
		return nil, err
	}

	return bytes, nil
}

func newBufferWriter(resolver Resolver, buffer *proto.Buffer, walker Walker, skipUnknown bool) *bufferWriter {
	return &bufferWriter{resolver: resolver, buffer: buffer, walker: walker, skipUnknown: skipUnknown}
}

func (w *bufferWriter) Visit(name string, data interface{}, field *descriptor.FieldDescriptorProto) (Visitor, bool, error) {
	switch field.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		v, ok := data.(string)
		if !ok {
			return nil, false, badTypeError(name, "string", data)
		}

		_ = w.buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), proto.WireBytes))
		_ = w.buffer.EncodeStringBytes(v)
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		v, ok := data.(float64)
		if !ok {
			return nil, false, badTypeError(name, "float64", data)
		}
		_ = w.buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), proto.WireFixed64))
		_ = w.buffer.EncodeFixed64(math.Float64bits(v))
	case descriptor.FieldDescriptorProto_TYPE_INT64:
		v, ok := data.(int)
		if !ok {
			return nil, false, badTypeError(name, "int", data)
		}
		_ = w.buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), proto.WireVarint))
		_ = w.buffer.EncodeVarint(uint64(v))
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		v, ok := data.(bool)
		if !ok {
			return nil, false, badTypeError(name, "bool", data)
		}
		val := uint64(0)
		if v {
			val = uint64(1)
		}
		_ = w.buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), proto.WireVarint))
		_ = w.buffer.EncodeVarint(val)
	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		enumValStr, ok := data.(string)
		if !ok {
			return nil, false, badTypeError(name, fmt.Sprintf("enum(%s)", strings.TrimPrefix(field.GetTypeName(), ".")), data)
		}
		enum := w.resolver.ResolveEnum(field.GetTypeName()) // enum must exist since resolver has full transitive closure.
		for _, val := range enum.Value {
			if val.GetName() == enumValStr {
				_ = w.buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), uint64(field.WireType())))
				_ = w.buffer.EncodeVarint(uint64(val.GetNumber()))
				return w, true, nil
			}
		}
		return nil, false, fmt.Errorf("unrecognized enum value '%s' for enum '%s'", enumValStr, enum.GetName())
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		buffer := GetBuffer()
		msgWriter := newBufferWriter(w.resolver, buffer, w.walker, w.skipUnknown)
		err := w.walker.Walk(data.(map[interface{}]interface{}), field.GetTypeName(), w.skipUnknown, msgWriter)
		bytes := buffer.Bytes()
		PutBuffer(buffer)

		if err != nil {
			return nil, false, err
		}
		_ = w.buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), uint64(field.WireType())))
		_ = w.buffer.EncodeRawBytes(bytes)

		return w, false, nil
	default:
		return nil, false, fmt.Errorf("unrecognized field type '%s'", (*field.Type).String())
	}

	return w, true, nil
}

func badTypeError(name, wantType string, data interface{}) error {
	return fmt.Errorf("field '%s' is of type '%T' instead of expected type '%s'", name, data, wantType)
}

func encodeIndexAndType(index int, typeid uint64) uint64 {
	return (uint64(index) << 3) | typeid
}
