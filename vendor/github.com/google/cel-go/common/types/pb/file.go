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

package pb

import (
	"fmt"

	descpb "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// FileDescription holds a map of all types and enums declared within a .proto
// file.
type FileDescription struct {
	desc  *descpb.FileDescriptorProto
	types map[string]*TypeDescription
	enums map[string]*EnumDescription
}

// GetEnumDescription returns an EnumDescription for a qualified enum value
// name declared within the .proto file.
func (fd *FileDescription) GetEnumDescription(enumName string) (*EnumDescription, error) {
	if ed, found := fd.enums[sanitizeProtoName(enumName)]; found {
		return ed, nil
	}
	return nil, fmt.Errorf("no such enum value '%s'", enumName)
}

// GetEnumNames returns the string names of all enum values in the file.
func (fd *FileDescription) GetEnumNames() []string {
	enumNames := make([]string, len(fd.enums))
	i := 0
	for _, e := range fd.enums {
		enumNames[i] = e.Name()
		i++
	}
	return enumNames
}

// GetTypeDescription returns a TypeDescription for a qualified type name
// declared within the .proto file.
func (fd *FileDescription) GetTypeDescription(typeName string) (*TypeDescription, error) {
	if td, found := fd.types[sanitizeProtoName(typeName)]; found {
		return td, nil
	}
	return nil, fmt.Errorf("no such type '%s'", typeName)
}

// GetTypeNames returns the list of all type names contained within the file.
func (fd *FileDescription) GetTypeNames() []string {
	typeNames := make([]string, len(fd.types))
	i := 0
	for _, t := range fd.types {
		typeNames[i] = t.Name()
		i++
	}
	return typeNames
}

// Package returns the file's qualified package name.
func (fd *FileDescription) Package() string {
	return fd.desc.GetPackage()
}

func (fd *FileDescription) indexEnums(pkg string, enumTypes []*descpb.EnumDescriptorProto) {
	for _, enumType := range enumTypes {
		for _, enumValue := range enumType.Value {
			enumValueName := fmt.Sprintf(
				"%s.%s.%s", pkg, enumType.GetName(), enumValue.GetName())
			fd.enums[enumValueName] = &EnumDescription{
				enumName: enumValueName,
				file:     fd,
				desc:     enumValue}
			revFileDescriptorMap[enumValueName] = fd
		}
	}
}

func (fd *FileDescription) indexTypes(pkg string, msgTypes []*descpb.DescriptorProto) {
	for _, msgType := range msgTypes {
		msgName := fmt.Sprintf("%s.%s", pkg, msgType.GetName())
		td := &TypeDescription{
			typeName:     msgName,
			file:         fd,
			desc:         msgType,
			fields:       make(map[string]*FieldDescription),
			fieldIndices: make(map[int][]*FieldDescription)}
		fd.types[msgName] = td
		fd.indexTypes(msgName, msgType.NestedType)
		fd.indexEnums(msgName, msgType.EnumType)
		revFileDescriptorMap[msgName] = fd
	}
}

func sanitizeProtoName(name string) string {
	if name != "" && name[0] == '.' {
		return name[1:]
	}
	return name
}
