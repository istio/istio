// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package types

import (
	"reflect"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/cel-go/common/types/pb"
	"github.com/google/cel-go/common/types/ref"

	descpb "github.com/golang/protobuf/protoc-gen-go/descriptor"
	anypb "github.com/golang/protobuf/ptypes/any"
	dpb "github.com/golang/protobuf/ptypes/duration"
	structpb "github.com/golang/protobuf/ptypes/struct"
	tpb "github.com/golang/protobuf/ptypes/timestamp"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

type protoTypeRegistry struct {
	revTypeMap map[string]ref.Type
	pbdb       *pb.Db
}

// NewRegistry accepts a list of proto message instances and returns a type
// provider which can create new instances of the provided message or any
// message that proto depends upon in its FileDescriptor.
func NewRegistry(types ...proto.Message) ref.TypeRegistry {
	p := &protoTypeRegistry{
		revTypeMap: make(map[string]ref.Type),
		pbdb:       pb.NewDb(),
	}
	p.RegisterType(
		BoolType,
		BytesType,
		DoubleType,
		DurationType,
		DynType,
		IntType,
		ListType,
		MapType,
		NullType,
		StringType,
		TimestampType,
		TypeType,
		UintType)

	for _, msgType := range types {
		fd, err := p.pbdb.RegisterMessage(msgType)
		if err != nil {
			panic(err)
		}
		for _, typeName := range fd.GetTypeNames() {
			p.RegisterType(NewObjectTypeValue(typeName))
		}
	}
	return p
}

func (p *protoTypeRegistry) EnumValue(enumName string) ref.Val {
	enumVal, err := p.pbdb.DescribeEnum(enumName)
	if err != nil {
		return NewErr("unknown enum name '%s'", enumName)
	}
	return Int(enumVal.Value())
}

func (p *protoTypeRegistry) FindFieldType(messageType string,
	fieldName string) (*ref.FieldType, bool) {
	msgType, err := p.pbdb.DescribeType(messageType)
	if err != nil {
		return nil, false
	}
	field, found := msgType.FieldByName(fieldName)
	if !found {
		return nil, false
	}
	return &ref.FieldType{
			Type:             field.CheckedType(),
			SupportsPresence: field.SupportsPresence()},
		true
}

func (p *protoTypeRegistry) FindIdent(identName string) (ref.Val, bool) {
	if t, found := p.revTypeMap[identName]; found {
		return t.(ref.Val), true
	}
	if enumVal, err := p.pbdb.DescribeEnum(identName); err == nil {
		return Int(enumVal.Value()), true
	}
	return nil, false
}

func (p *protoTypeRegistry) FindType(typeName string) (*exprpb.Type, bool) {
	if _, err := p.pbdb.DescribeType(typeName); err != nil {
		return nil, false
	}
	if typeName != "" && typeName[0] == '.' {
		typeName = typeName[1:]
	}
	return &exprpb.Type{
		TypeKind: &exprpb.Type_Type{
			Type: &exprpb.Type{
				TypeKind: &exprpb.Type_MessageType{
					MessageType: typeName}}}}, true
}

func (p *protoTypeRegistry) NewValue(typeName string, fields map[string]ref.Val) ref.Val {
	td, err := p.pbdb.DescribeType(typeName)
	if err != nil {
		return NewErr("unknown type '%s'", typeName)
	}
	refType := td.ReflectType()
	// create the new type instance.
	value := reflect.New(refType.Elem())
	pbValue := value.Elem()

	// for all of the field names referenced, set the provided value.
	for name, value := range fields {
		fd, found := td.FieldByName(name)
		if !found {
			return NewErr("no such field '%s'", name)
		}
		refField := pbValue.Field(fd.Index())
		if !refField.IsValid() {
			return NewErr("no such field '%s'", name)
		}

		dstType := refField.Type()
		// Oneof fields are defined with wrapper structs that have a single proto.Message
		// field value. The oneof wrapper is not a proto.Message instance.
		if fd.IsOneof() {
			oneofVal := reflect.New(fd.OneofType().Elem())
			refField.Set(oneofVal)
			refField = oneofVal.Elem().Field(0)
			dstType = refField.Type()
		}
		fieldValue, err := value.ConvertToNative(dstType)
		if err != nil {
			return &Err{err}
		}
		refField.Set(reflect.ValueOf(fieldValue))
	}
	return NewObject(p, td, value.Interface().(proto.Message))
}

func (p *protoTypeRegistry) RegisterDescriptor(fileDesc *descpb.FileDescriptorProto) error {
	fd, err := p.pbdb.RegisterDescriptor(fileDesc)
	if err != nil {
		return err
	}
	return p.registerAllTypes(fd)
}

func (p *protoTypeRegistry) RegisterMessage(message proto.Message) error {
	fd, err := p.pbdb.RegisterMessage(message)
	if err != nil {
		return err
	}
	return p.registerAllTypes(fd)
}

func (p *protoTypeRegistry) RegisterType(types ...ref.Type) error {
	for _, t := range types {
		p.revTypeMap[t.TypeName()] = t
	}
	// TODO: generate an error when the type name is registered more than once.
	return nil
}

func (p *protoTypeRegistry) registerAllTypes(fd *pb.FileDescription) error {
	for _, typeName := range fd.GetTypeNames() {
		err := p.RegisterType(NewObjectTypeValue(typeName))
		if err != nil {
			return err
		}
	}
	return nil
}

// NativeToValue converts various "native" types to ref.Val with this specific implementation
// providing support for custom proto-based types.
//
// This method should be the inverse of ref.Val.ConvertToNative.
func (p *protoTypeRegistry) NativeToValue(value interface{}) ref.Val {
	switch value.(type) {
	case ref.Val:
		return value.(ref.Val)
	// Adapt common types and aggregate specializations using the DefaultTypeAdapter.
	case bool, *bool,
		float32, *float32, float64, *float64,
		int, *int, int32, *int32, int64, *int64,
		string, *string,
		uint, *uint, uint32, *uint32, uint64, *uint64,
		[]byte,
		[]string,
		map[string]string:
		return DefaultTypeAdapter.NativeToValue(value)
	// Adapt well-known proto-types using the DefaultTypeAdapter.
	case *dpb.Duration,
		*tpb.Timestamp,
		*structpb.ListValue,
		structpb.NullValue,
		*structpb.Struct,
		*structpb.Value:
		return DefaultTypeAdapter.NativeToValue(value)
	// Override the Any type by ensuring that custom proto-types are considered on recursive calls.
	case *anypb.Any:
		val := value.(*anypb.Any)
		unpackedAny := ptypes.DynamicAny{}
		if ptypes.UnmarshalAny(val, &unpackedAny) != nil {
			NewErr("Fail to unmarshal any.")
		}
		return p.NativeToValue(unpackedAny.Message)
	// Convert custom proto types to CEL values based on type's presence within the pb.Db.
	case proto.Message:
		pbVal := value.(proto.Message)
		typeName := proto.MessageName(pbVal)
		td, err := p.pbdb.DescribeType(typeName)
		if err != nil {
			return NewErr("unknown type '%s'", typeName)
		}
		return NewObject(p, td, pbVal)
	// Override default handling for list and maps to ensure that blends of Go + proto types
	// are appropriately adapted on recursive calls or subsequent inspection of the aggregate
	// value.
	default:
		refValue := reflect.ValueOf(value)
		if refValue.Kind() == reflect.Ptr {
			refValue = refValue.Elem()
		}
		refKind := refValue.Kind()
		switch refKind {
		case reflect.Array, reflect.Slice:
			return NewDynamicList(p, value)
		case reflect.Map:
			return NewDynamicMap(p, value)
		}
	}
	// By default return the default type adapter's conversion to CEL.
	return DefaultTypeAdapter.NativeToValue(value)
}

// defaultTypeAdapter converts go native types to CEL values.
type defaultTypeAdapter struct{}

var (
	// DefaultTypeAdapter adapts canonical CEL types from their equivalent Go values.
	DefaultTypeAdapter = &defaultTypeAdapter{}
)

// NativeToValue implements the ref.TypeAdapter interface.
func (a *defaultTypeAdapter) NativeToValue(value interface{}) ref.Val {
	switch value.(type) {
	case ref.Val:
		return value.(ref.Val)
	case bool:
		return Bool(value.(bool))
	case *bool:
		return Bool(*value.(*bool))
	case int:
		return Int(value.(int))
	case int32:
		return Int(value.(int32))
	case int64:
		return Int(value.(int64))
	case *int:
		return Int(*value.(*int))
	case *int32:
		return Int(*value.(*int32))
	case *int64:
		return Int(*value.(*int64))
	case uint:
		return Uint(value.(uint))
	case uint32:
		return Uint(value.(uint32))
	case uint64:
		return Uint(value.(uint64))
	case *uint:
		return Uint(*value.(*uint))
	case *uint32:
		return Uint(*value.(*uint32))
	case *uint64:
		return Uint(*value.(*uint64))
	case float32:
		return Double(value.(float32))
	case float64:
		return Double(value.(float64))
	case *float32:
		return Double(*value.(*float32))
	case *float64:
		return Double(*value.(*float64))
	case string:
		return String(value.(string))
	case *string:
		return String(*value.(*string))
	case []byte:
		return Bytes(value.([]byte))
	case []string:
		return NewStringList(a, value.([]string))
	case map[string]string:
		return NewStringStringMap(value.(map[string]string))
	case *dpb.Duration:
		return Duration{value.(*dpb.Duration)}
	case *structpb.ListValue:
		return NewJSONList(a, value.(*structpb.ListValue))
	case structpb.NullValue:
		return NullValue
	case *structpb.Struct:
		return NewJSONStruct(a, value.(*structpb.Struct))
	case *structpb.Value:
		v := value.(*structpb.Value)
		switch v.Kind.(type) {
		case *structpb.Value_BoolValue:
			return a.NativeToValue(v.GetBoolValue())
		case *structpb.Value_ListValue:
			return a.NativeToValue(v.GetListValue())
		case *structpb.Value_NullValue:
			return NullValue
		case *structpb.Value_NumberValue:
			return a.NativeToValue(v.GetNumberValue())
		case *structpb.Value_StringValue:
			return a.NativeToValue(v.GetStringValue())
		case *structpb.Value_StructValue:
			return a.NativeToValue(v.GetStructValue())
		}
	case *tpb.Timestamp:
		return Timestamp{value.(*tpb.Timestamp)}
	case *anypb.Any:
		val := value.(*anypb.Any)
		unpackedAny := ptypes.DynamicAny{}
		if ptypes.UnmarshalAny(val, &unpackedAny) != nil {
			NewErr("Fail to unmarshal any.")
		}
		return a.NativeToValue(unpackedAny.Message)
	default:
		refValue := reflect.ValueOf(value)
		if refValue.Kind() == reflect.Ptr {
			refValue = refValue.Elem()
		}
		refKind := refValue.Kind()
		switch refKind {
		case reflect.Array, reflect.Slice:
			return NewDynamicList(a, value)
		case reflect.Map:
			return NewDynamicMap(a, value)
		// type aliases of primitive types cannot be asserted as that type, but rather need
		// to be downcast to int32 before being converted to a CEL representation.
		case reflect.Int32:
			intType := reflect.TypeOf(int32(0))
			return Int(refValue.Convert(intType).Interface().(int32))
		case reflect.Int64:
			intType := reflect.TypeOf(int64(0))
			return Int(refValue.Convert(intType).Interface().(int64))
		case reflect.Uint32:
			uintType := reflect.TypeOf(uint32(0))
			return Uint(refValue.Convert(uintType).Interface().(uint32))
		case reflect.Uint64:
			uintType := reflect.TypeOf(uint64(0))
			return Uint(refValue.Convert(uintType).Interface().(uint64))
		case reflect.Float32:
			doubleType := reflect.TypeOf(float32(0))
			return Double(refValue.Convert(doubleType).Interface().(float32))
		case reflect.Float64:
			doubleType := reflect.TypeOf(float64(0))
			return Double(refValue.Convert(doubleType).Interface().(float64))
		}
	}
	return NewErr("unsupported type conversion for value '%v'", value)
}
