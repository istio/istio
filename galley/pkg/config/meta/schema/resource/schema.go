// Copyright Istio Authors
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

package resource

import (
	"fmt"
	"reflect"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/config/validation"
)

// Schema describes a resource
type Schema struct {
	// ClusterScoped is true for resource in cluster-level.
	ClusterScoped bool

	// Type is the config proto type.
	Kind string

	// Plural is the type in plural.
	Plural string

	// Group is the config proto group.
	Group string

	// Version is the config proto version.
	Version string

	// Proto refers to the protobuf message type name corresponding to the type
	Proto string

	// ProtoPackage refers to the name of golang package for the protobuf message.
	ProtoPackage string

	// ValidateProto performs validation on protobuf messages based on this schema.
	ValidateProto validation.ValidateFunc
}

// Equal is a helper function for testing equality between Schema instances. This explicitly ignores the
// ValidateProto field in order to support the cmp library.
func (s Schema) Equal(o Schema) bool {
	return s.ClusterScoped == o.ClusterScoped &&
		s.Kind == o.Kind &&
		s.Plural == o.Plural &&
		s.Group == o.Group &&
		s.Version == o.Version &&
		s.Proto == o.Proto &&
		s.ProtoPackage == o.ProtoPackage
}

// CanonicalResourceName of the resource.
func (s Schema) CanonicalResourceName() string {
	if s.Group == "" {
		return "core/" + s.Version + "/" + s.Kind
	}
	return s.Group + "/" + s.Version + "/" + s.Kind
}

// Validate the schemas. Returns error if there is a problem.
func (s *Schema) Validate() error {
	if getProtoMessageType(s.Proto) == nil {
		return fmt.Errorf("proto message not found: %v", s.Proto)
	}
	return nil
}

// String interface method implementation.
func (s *Schema) String() string {
	return fmt.Sprintf("[Schema](%s, %q, %s)", s.Kind, s.ProtoPackage, s.Proto)
}

// NewProtoInstance returns a new instance of the underlying proto for this resource.
func (s *Schema) NewProtoInstance() proto.Message {
	goType := getProtoMessageType(s.Proto)
	if goType == nil {
		panic(fmt.Errorf("message not found: %q", s.Proto))
	}

	instance := reflect.New(goType).Interface()

	if p, ok := instance.(proto.Message); !ok {
		panic(fmt.Sprintf(
			"NewProtoInstance: message is not an instance of proto.Message. kind:%s, type:%v, value:%v",
			s.Kind, goType, instance))
	} else {
		return p
	}
}

// getProtoMessageType returns the Go lang type of the proto with the specified name.
func getProtoMessageType(protoMessageName string) reflect.Type {
	t := protoMessageType(protoMessageName)
	if t == nil {
		return nil
	}
	return t.Elem()
}

var protoMessageType = proto.MessageType
