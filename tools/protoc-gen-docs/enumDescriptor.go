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
	*descriptor.EnumDescriptorProto
	file     *fileDescriptor        // File this coreDesc comes from
	values   []*enumValueDescriptor // The values of this enum
	typename []string
	loc      *descriptor.SourceCodeInfo_Location
}

type enumValueDescriptor struct {
	*descriptor.EnumValueDescriptorProto
	loc *descriptor.SourceCodeInfo_Location
}

func newEnumDescriptor(desc *descriptor.EnumDescriptorProto, parent *messageDescriptor, file *fileDescriptor, path pathVector) *enumDescriptor {
	e := &enumDescriptor{
		EnumDescriptorProto: desc,
		file:                file,
		loc:                 file.find(path),
	}

	if parent == nil {
		e.typename = []string{desc.GetName()}
	} else {
		pname := parent.typeName()
		e.typename = make([]string, len(pname)+1)
		copy(e.typename, pname)
		e.typename[len(e.typename)-1] = desc.GetName()
	}

	e.values = make([]*enumValueDescriptor, 0, len(desc.Value))
	for i, ev := range desc.Value {
		evd := &enumValueDescriptor{
			EnumValueDescriptorProto: ev,
			loc: file.find(path.append(enumValuePath, i)),
		}
		e.values = append(e.values, evd)
	}

	return e
}

func (e *enumDescriptor) fileDesc() *fileDescriptor {
	return e.file
}

func (e *enumDescriptor) typeName() (s []string) {
	return e.typename
}
