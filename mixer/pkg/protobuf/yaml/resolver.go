// Copyright Istio Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package yaml

import (
	"strings"

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"

	descriptor2 "istio.io/istio/mixer/pkg/protobuf/descriptor"
)

// Resolver type is used for finding and resolving references inside proto descriptors.
type Resolver interface {
	ResolveMessage(name string) *descriptor.DescriptorProto
	ResolveEnum(name string) *descriptor.EnumDescriptorProto

	// Resolve service contained in the proto
	ResolveService(namePrefix string) (svc *descriptor.ServiceDescriptorProto, pkg string)
}

type resolver struct {
	messages map[string]*descriptor.DescriptorProto
	enums    map[string]*descriptor.EnumDescriptorProto

	parser *descriptor2.FileDescriptorSetParser
}

// NewResolver creates a new resolver
func NewResolver(fds *descriptor.FileDescriptorSet) Resolver {
	parser := descriptor2.CreateFileDescriptorSetParser(fds, nil, "")
	return &resolver{enums: parser.Enums(), messages: parser.Messages(), parser: parser}
}

// ResolveMessage finds a given fully-qualified message name. The name starts with
// a dot followed by package name and the name of the element. Example: `.foo.mymessage.mysubmessage`
func (r *resolver) ResolveMessage(name string) *descriptor.DescriptorProto {
	return r.messages[name]
}

// ResolveService finds a service that starts with a prefix
// Example: `.metric.HandleMetricService`
func (r *resolver) ResolveService(namePrefix string) (svc *descriptor.ServiceDescriptorProto, pkg string) {
	for _, fd := range r.parser.AllFiles {
		for idx := range fd.Service {
			svc = fd.Service[idx]
			pkg = fd.GetPackage()
			if strings.HasPrefix(svc.GetName(), namePrefix) {
				return svc, pkg
			}
		}
	}
	return nil, ""
}

// ResolveEnum finds a given fully-qualified enum name. The name starts with
// a dot followed by package name and the name of the element. Example: `.foo.mymessage.myenum`
func (r *resolver) ResolveEnum(name string) *descriptor.EnumDescriptorProto {
	return r.enums[name]
}

// FindFieldByName finds a field descriptor by its name.
func FindFieldByName(descriptor *descriptor.DescriptorProto, name string) *descriptor.FieldDescriptorProto {
	for _, f := range descriptor.Field {
		if f.GetJsonName() == name || f.GetName() == name {
			return f
		}
	}
	return nil
}
