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

type messageDescriptor struct {
	baseDesc
	*descriptor.DescriptorProto
	parent   *messageDescriptor   // The containing message, if any
	messages []*messageDescriptor // Inner messages, if any
	enums    []*enumDescriptor    // Inner enums, if any
	fields   []*fieldDescriptor   // Fields, if any
}

type fieldDescriptor struct {
	baseDesc
	*descriptor.FieldDescriptorProto
	typ coreDesc // Type of data held by this field
}

func newMessageDescriptor(desc *descriptor.DescriptorProto, parent *messageDescriptor, file *fileDescriptor, path pathVector) *messageDescriptor {
	var qualifiedName []string
	if parent == nil {
		qualifiedName = []string{desc.GetName()}
	} else {
		qualifiedName = make([]string, len(parent.qualifiedName()), len(parent.qualifiedName())+1)
		copy(qualifiedName, parent.qualifiedName())
		qualifiedName = append(qualifiedName, desc.GetName())
	}

	m := &messageDescriptor{
		DescriptorProto: desc,
		parent:          parent,
		baseDesc:        newBaseDesc(file, path, qualifiedName),
	}

	for i, f := range desc.Field {
		nameCopy := make([]string, len(qualifiedName), len(qualifiedName)+1)
		copy(nameCopy, qualifiedName)
		nameCopy = append(nameCopy, f.GetName())

		fd := &fieldDescriptor{
			FieldDescriptorProto: f,
			baseDesc:             newBaseDesc(file, path.append(messageFieldPath, i), nameCopy),
		}

		m.fields = append(m.fields, fd)
	}

	for i, msg := range desc.NestedType {
		m.messages = append(m.messages, newMessageDescriptor(msg, m, file, path.append(messageMessagePath, i)))
	}

	for i, e := range desc.EnumType {
		m.enums = append(m.enums, newEnumDescriptor(e, m, file, path.append(messageEnumPath, i)))
	}

	return m
}

func (f *fieldDescriptor) isRepeated() bool {
	return f.Label != nil && *f.Label == descriptor.FieldDescriptorProto_LABEL_REPEATED
}
