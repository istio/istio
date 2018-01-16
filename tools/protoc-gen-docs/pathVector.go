// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this currentFile except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"strconv"
)

// The SourceCodeInfo messages describes the location of elements of a parsed
// .proto currentFile by way of a "path", which is a sequence of integers that
// describe the route from a FileDescriptorProto to the relevant submessage.
// The path alternates between a field number of a repeated field, and an index
// into that repeated field. The constants below define the field numbers that
// are used.
//
// See descriptor.proto for more information about this.
const (
	// tag numbers in FileDescriptorProto
	packagePath = 2 // package
	messagePath = 4 // message_type
	enumPath    = 5 // enum_type
	servicePath = 6 // service

	// tag numbers in DescriptorProto
	messageFieldPath   = 2 // field
	messageMessagePath = 3 // nested_type
	messageEnumPath    = 4 // enum_type

	// tag numbers in EnumDescriptorProto
	enumValuePath = 2 // value

	// tag numbers in ServiceDescriptorProto
	serviceMethodPath = 2 // method
)

// A vector of comma-separated integers which identify a particular entry in a
// given's file location information
type pathVector string

func newPathVector(v int) pathVector {
	return pathVector(strconv.Itoa(v))
}

func (pv pathVector) append(v ...int) pathVector {
	result := pv
	for _, val := range v {
		result = result + pathVector(","+strconv.Itoa(val))
	}
	return result
}
