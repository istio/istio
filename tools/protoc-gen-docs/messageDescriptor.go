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
	*descriptor.DescriptorProto
	file     *fileDescriptor      // File this coreDesc comes from
	parent   *messageDescriptor   // The containing message, if any
	messages []*messageDescriptor // Inner messages, if any
	enums    []*enumDescriptor    // Inner enums, if any
	fields   []*fieldDescriptor   // Fields, if any
	typename []string
	loc      *descriptor.SourceCodeInfo_Location
}

type fieldDescriptor struct {
	*descriptor.FieldDescriptorProto
	typ coreDesc // Type of data held by this field
	loc *descriptor.SourceCodeInfo_Location
}

func newMessageDescriptor(desc *descriptor.DescriptorProto, parent *messageDescriptor, file *fileDescriptor, path pathVector) *messageDescriptor {
	m := &messageDescriptor{
		DescriptorProto: desc,
		file:            file,
		parent:          parent,
		loc:             file.find(path),
	}

	n := 0
	for p := m; p != nil; p = p.parent {
		n++
	}

	m.typename = make([]string, n, n)
	for p := m; p != nil; p = p.parent {
		n--
		m.typename[n] = p.GetName()
	}

	for i, f := range desc.Field {
		fd := &fieldDescriptor{
			FieldDescriptorProto: f,
			loc:                  file.find(path.append(messageFieldPath, i)),
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

func (m *messageDescriptor) fileDesc() *fileDescriptor {
	return m.file
}

func (m *messageDescriptor) typeName() []string {
	return m.typename
}

func (f *fieldDescriptor) isRepeated() bool {
	return f.Label != nil && *f.Label == descriptor.FieldDescriptorProto_LABEL_REPEATED
}
