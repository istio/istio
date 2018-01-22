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
	baseDesc
	*descriptor.ServiceDescriptorProto
	methods []*methodDescriptor // Methods, if any
}

type methodDescriptor struct {
	baseDesc
	*descriptor.MethodDescriptorProto
	input  *messageDescriptor
	output *messageDescriptor
}

func newServiceDescriptor(desc *descriptor.ServiceDescriptorProto, file *fileDescriptor, path pathVector) *serviceDescriptor {
	qualifiedName := []string{desc.GetName()}

	s := &serviceDescriptor{
		ServiceDescriptorProto: desc,
		baseDesc:               newBaseDesc(file, path, qualifiedName),
	}

	for i, m := range desc.Method {
		md := &methodDescriptor{
			MethodDescriptorProto: m,
			baseDesc:              newBaseDesc(file, path.append(serviceMethodPath, i), append(qualifiedName, m.GetName())),
		}
		s.methods = append(s.methods, md)
	}

	return s
}
