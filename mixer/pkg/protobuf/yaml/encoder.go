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
	// Encoder transforms yaml that represents protobuf data into []byte
	Encoder struct {
		resolver Resolver
	}
)

// NewEncoder creates a Encoder
func NewEncoder(fds *descriptor.FileDescriptorSet) *Encoder {
	resolver := NewResolver(fds)
	return &Encoder{resolver: resolver}
}

// EncodeBytes creates []byte from a yaml representation of a proto.
func (e *Encoder) EncodeBytes(data map[interface{}]interface{}, msgName string, skipUnknown bool) ([]byte, error) {
	buf := GetBuffer()
	defer func() { PutBuffer(buf) }()

	message := e.resolver.ResolveMessage(msgName)
	if message == nil {
		return nil, fmt.Errorf("cannot resolve message '%s'", msgName)
	}
	for k, v := range data {
		fd := findFieldByName(message, k.(string))
		if fd == nil {
			if skipUnknown {
				continue
			}
			return nil, fmt.Errorf("field '%s' not found in message '%s'", k, message.GetName())
		}

		if err := e.visit(k.(string), v, fd, skipUnknown, buf); err != nil {
			return nil, err
		}

	}
	return buf.Bytes(), nil
}

func (e *Encoder) visit(name string, data interface{}, field *descriptor.FieldDescriptorProto, skipUnknown bool, buffer *proto.Buffer) error {
	switch field.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		v, ok := data.(string)
		if !ok {
			return badTypeError(name, "string", data)
		}

		// Errors from proto.Buffer.Encode* are always nil, therefore ignoring.
		_ = buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), proto.WireBytes))
		_ = buffer.EncodeStringBytes(v)
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		v, ok := data.(float64)
		if !ok {
			return badTypeError(name, "float64", data)
		}
		_ = buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), proto.WireFixed64))
		_ = buffer.EncodeFixed64(math.Float64bits(v))
	case descriptor.FieldDescriptorProto_TYPE_INT64:
		v, ok := data.(int)
		if !ok {
			return badTypeError(name, "int", data)
		}
		_ = buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), proto.WireVarint))
		_ = buffer.EncodeVarint(uint64(v))
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		v, ok := data.(bool)
		if !ok {
			return badTypeError(name, "bool", data)
		}
		val := uint64(0)
		if v {
			val = uint64(1)
		}
		_ = buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), proto.WireVarint))
		_ = buffer.EncodeVarint(val)
	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		enumValStr, ok := data.(string)
		if !ok {
			return badTypeError(name, fmt.Sprintf("enum(%s)", strings.TrimPrefix(field.GetTypeName(), ".")), data)
		}
		enum := e.resolver.ResolveEnum(field.GetTypeName()) // enum must exist since resolver has full transitive closure.
		for _, val := range enum.Value {
			if val.GetName() == enumValStr {
				_ = buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), uint64(field.WireType())))
				_ = buffer.EncodeVarint(uint64(val.GetNumber()))
				return nil
			}
		}
		return fmt.Errorf("unrecognized enum value '%s' for enum '%s'", enumValStr, enum.GetName())
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		v, ok := data.(map[interface{}]interface{})
		if !ok {
			return badTypeError(name, strings.TrimPrefix(field.GetTypeName(), "."), data)
		}

		bytes, err := e.EncodeBytes(v, field.GetTypeName(), skipUnknown)
		if err != nil {
			return err
		}
		_ = buffer.EncodeVarint(encodeIndexAndType(int(field.GetNumber()), uint64(field.WireType())))
		_ = buffer.EncodeRawBytes(bytes)

		return nil
	default:
		return fmt.Errorf("unrecognized field type '%s'", (*field.Type).String())
	}

	return nil
}

func badTypeError(name, wantType string, data interface{}) error {
	return fmt.Errorf("field '%s' is of type '%T' instead of expected type '%s'", name, data, wantType)
}

func encodeIndexAndType(index int, typeid uint64) uint64 {
	return (uint64(index) << 3) | typeid
}
