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
	writer interface {
		write(buffer *proto.Buffer) error
	}
	msgWriter struct {
		field    *descriptor.FieldDescriptorProto // nil for root message
		name     string                           // empty for root message
		resolver Resolver
		children []writer
	}
	primitiveWriter struct {
		field    *descriptor.FieldDescriptorProto
		data     interface{}
		name     string
		resolver Resolver
	}
	// Encoder transforms yaml that represents protobuf data into []byte
	Encoder struct {
		reader   Walker
		resolver Resolver
	}
)

// NewEncoder creates a Encoder
func NewEncoder(fds *descriptor.FileDescriptorSet) *Encoder {
	resolver := NewResolver(fds)
	return &Encoder{resolver: resolver, reader: NewWalker(resolver)}
}

// EncodeBytes creates []byte from a yaml representation of a proto.
func (c *Encoder) EncodeBytes(data map[interface{}]interface{}, msgName string, skipUnknown bool) ([]byte, error) {
	w := newMsgWriter("", nil, c.resolver)
	if err := c.reader.Walk(data, msgName, skipUnknown, w); err != nil {
		return nil, err
	}

	buf := GetBuffer()
	err := w.write(buf)
	bytes := buf.Bytes()
	PutBuffer(buf)

	if err != nil {
		return nil, err
	}

	return bytes, nil
}

func (w *primitiveWriter) write(buffer *proto.Buffer) error {
	switch *w.field.Type {
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		v, ok := w.data.(string)
		if !ok {
			return badTypeError(w.name, "string", w.data)
		}

		_ = buffer.EncodeVarint(encodeIndexAndType(int(w.field.GetNumber()), proto.WireBytes))
		_ = buffer.EncodeStringBytes(v)
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		v, ok := w.data.(float64)
		if !ok {
			return badTypeError(w.name, "float64", w.data)
		}
		_ = buffer.EncodeVarint(encodeIndexAndType(int(w.field.GetNumber()), proto.WireFixed64))
		_ = buffer.EncodeFixed64(math.Float64bits(v))
	case descriptor.FieldDescriptorProto_TYPE_INT64:
		v, ok := w.data.(int)
		if !ok {
			return badTypeError(w.name, "int", w.data)
		}
		_ = buffer.EncodeVarint(encodeIndexAndType(int(w.field.GetNumber()), proto.WireVarint))
		_ = buffer.EncodeVarint(uint64(v))
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		v, ok := w.data.(bool)
		if !ok {
			return badTypeError(w.name, "bool", w.data)
		}
		val := uint64(0)
		if v {
			val = uint64(1)
		}
		_ = buffer.EncodeVarint(encodeIndexAndType(int(w.field.GetNumber()), proto.WireVarint))
		_ = buffer.EncodeVarint(val)
	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		enumValStr, ok := w.data.(string)
		if !ok {
			return badTypeError(w.name, fmt.Sprintf("enum(%s)", strings.TrimPrefix(w.field.GetTypeName(), ".")), w.data)
		}
		enum := w.resolver.ResolveEnum(w.field.GetTypeName()) // enum must exist since resolver has full transitive closure.
		for _, val := range enum.Value {
			if val.GetName() == enumValStr {
				_ = buffer.EncodeVarint(encodeIndexAndType(int(w.field.GetNumber()), uint64(w.field.WireType())))
				_ = buffer.EncodeVarint(uint64(val.GetNumber()))
				return nil
			}
		}
		return fmt.Errorf("unrecognized enum value '%s' for enum '%s'", enumValStr, enum.GetName())
	}

	return nil
}

func (w *msgWriter) write(buffer *proto.Buffer) error {
	origin := buffer
	if w.field != nil {
		buffer = GetBuffer()
		defer func() { PutBuffer(buffer) }()
	}
	for _, child := range w.children {
		if err := child.write(buffer); err != nil {
			return err
		}
	}
	if w.field != nil {
		_ = origin.EncodeVarint(encodeIndexAndType(int(w.field.GetNumber()), uint64(w.field.WireType())))
		_ = origin.EncodeRawBytes(buffer.Bytes())
	}

	return nil
}

func newMsgWriter(name string, field *descriptor.FieldDescriptorProto, resolver Resolver) *msgWriter {
	return &msgWriter{children: make([]writer, 0), name: name, field: field, resolver: resolver}
}

func (w *msgWriter) Visit(name string, val interface{}, field *descriptor.FieldDescriptorProto) (Visitor, error) {
	switch field.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		child := newMsgWriter(name, field, w.resolver)
		w.children = append(w.children, child)
		return child, nil
	default:
		child := primitiveWriter{resolver: w.resolver, name: name, field: field, data: val}
		w.children = append(w.children, &child)
		return w, nil
	}
}

func badTypeError(name, wantType string, data interface{}) error {
	return fmt.Errorf("field '%s' is of type '%T' instead of expected type '%s'", name, data, wantType)
}

func encodeIndexAndType(index int, typeid uint64) uint64 {
	return (uint64(index) << 3) | typeid
}
