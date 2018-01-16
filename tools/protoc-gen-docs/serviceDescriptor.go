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

type serviceDescriptor struct {
	*descriptor.ServiceDescriptorProto
	file     *fileDescriptor     // File this coreDesc comes from
	methods  []*methodDescriptor // Methods, if any
	typename []string
	loc      *descriptor.SourceCodeInfo_Location
}

type methodDescriptor struct {
	*descriptor.MethodDescriptorProto
	input  *messageDescriptor
	output *messageDescriptor
	loc    *descriptor.SourceCodeInfo_Location
}

func newServiceDescriptor(desc *descriptor.ServiceDescriptorProto, file *fileDescriptor, path pathVector) *serviceDescriptor {
	var methods []*methodDescriptor
	for i, m := range desc.Method {
		md := &methodDescriptor{
			MethodDescriptorProto: m,
			loc: file.find(path.append(serviceMethodPath, i)),
		}
		methods = append(methods, md)
	}

	return &serviceDescriptor{
		ServiceDescriptorProto: desc,
		file:     file,
		loc:      file.find(path),
		typename: []string{desc.GetName()},
		methods:  methods,
	}
}

func (s *serviceDescriptor) fileDesc() *fileDescriptor {
	return s.file
}

func (s *serviceDescriptor) typeName() []string {
	return s.typename
}
