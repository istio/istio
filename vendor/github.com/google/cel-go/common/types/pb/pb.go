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

// Package pb reflects over protocol buffer descriptors to generate objects
// that simplify type, enum, and field lookup.
package pb

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"

	"github.com/golang/protobuf/descriptor"
	"github.com/golang/protobuf/proto"
	descpb "github.com/golang/protobuf/protoc-gen-go/descriptor"
	anypb "github.com/golang/protobuf/ptypes/any"
	durpb "github.com/golang/protobuf/ptypes/duration"
	structpb "github.com/golang/protobuf/ptypes/struct"
	tspb "github.com/golang/protobuf/ptypes/timestamp"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
)

// DescribeEnum takes a qualified enum name and returns an EnumDescription.
func DescribeEnum(enumName string) (*EnumDescription, error) {
	enumName = sanitizeProtoName(enumName)
	if fd, found := revFileDescriptorMap[enumName]; found {
		return fd.GetEnumDescription(enumName)
	}
	return nil, fmt.Errorf("unrecognized enum '%s'", enumName)
}

// DescribeFile takes a protocol buffer message and indexes all of the message
// types and enum values contained within the message's file descriptor.
func DescribeFile(message proto.Message) (*FileDescription, error) {
	if fd, found := revFileDescriptorMap[proto.MessageName(message)]; found {
		return fd, nil
	}
	fileDesc, _ := descriptor.ForMessage(message.(descriptor.Message))
	fd, err := describeFileInternal(fileDesc)
	if err != nil {
		return nil, err
	}
	pkg := fd.Package()
	fd.indexTypes(pkg, fileDesc.MessageType)
	fd.indexEnums(pkg, fileDesc.EnumType)
	return fd, nil
}

// DescribeType provides a TypeDescription given a qualified type name.
func DescribeType(typeName string) (*TypeDescription, error) {
	typeName = sanitizeProtoName(typeName)
	if fd, found := revFileDescriptorMap[typeName]; found {
		return fd.GetTypeDescription(typeName)
	}
	return nil, fmt.Errorf("unrecognized type '%s'", typeName)
}

// DescribeValue takes an instance of a protocol buffer message and returns
// the associated TypeDescription.
func DescribeValue(value proto.Message) (*TypeDescription, error) {
	fd, err := DescribeFile(value)
	if err != nil {
		return nil, err
	}
	typeName := proto.MessageName(value)
	return fd.GetTypeDescription(typeName)
}

var (
	// map from file / message / enum name to file description.
	fileDescriptorMap    = make(map[string]*FileDescription)
	revFileDescriptorMap = make(map[string]*FileDescription)
)

func describeFileInternal(fileDesc *descpb.FileDescriptorProto) (*FileDescription, error) {
	fd := &FileDescription{
		desc:  fileDesc,
		types: make(map[string]*TypeDescription),
		enums: make(map[string]*EnumDescription)}
	fileDescriptorMap[fileDesc.GetName()] = fd

	for _, dep := range fileDesc.Dependency {
		if _, found := fileDescriptorMap[dep]; !found {
			nestedDesc, err := fileDescriptor(dep)
			if err != nil {
				panic(err)
			}
			describeFileInternal(nestedDesc)
		}
	}

	return fd, nil
}

func fileDescriptor(protoFileName string) (*descpb.FileDescriptorProto, error) {
	gzipped := proto.FileDescriptor(protoFileName)
	r, err := gzip.NewReader(bytes.NewReader(gzipped))
	if err != nil {
		return nil, fmt.Errorf("bad gzipped descriptor: %v", err)
	}
	unzipped, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("bad gzipped descriptor: %v", err)
	}
	fd := &descpb.FileDescriptorProto{}
	if err := proto.Unmarshal(unzipped, fd); err != nil {
		return nil, fmt.Errorf("bad gzipped descriptor: %v", err)
	}
	return fd, nil
}

func init() {
	// Describe well-known types to ensure they can always be resolved by the check and interpret
	// execution phases.
	//
	// The following subset of message types is enough to ensure that all well-known types can
	// resolved in the runtime, since describing the value results in describing the whole file
	// where the message is declared.
	DescribeValue(&anypb.Any{})
	DescribeValue(&durpb.Duration{})
	DescribeValue(&tspb.Timestamp{})
	DescribeValue(&structpb.Value{})
	DescribeValue(&wrapperspb.BoolValue{})
}
