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
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
)

type enumDescriptor struct {
	baseDesc
	*descriptor.EnumDescriptorProto
	values []*enumValueDescriptor // The values of this enum
}

type enumValueDescriptor struct {
	baseDesc
	*descriptor.EnumValueDescriptorProto
}

func newEnumDescriptor(desc *descriptor.EnumDescriptorProto, parent *messageDescriptor, file *fileDescriptor, path pathVector) *enumDescriptor {
	var qualifiedName []string
	if parent == nil {
		qualifiedName = []string{desc.GetName()}
	} else {
		qualifiedName = append(parent.qualifiedName(), desc.GetName())
	}

	e := &enumDescriptor{
		EnumDescriptorProto: desc,
		baseDesc:            newBaseDesc(file, path, qualifiedName),
	}

	e.values = make([]*enumValueDescriptor, 0, len(desc.Value))
	for i, ev := range desc.Value {
		evd := &enumValueDescriptor{
			EnumValueDescriptorProto: ev,
			baseDesc:                 newBaseDesc(file, path.append(enumValuePath, i), append(qualifiedName, ev.GetName())),
		}
		e.values = append(e.values, evd)
	}

	return e
}
