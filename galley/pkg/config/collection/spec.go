// Copyright 2019 Istio Authors
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

package collection

import (
	"fmt"
	"reflect"

	"github.com/gogo/protobuf/proto"
)

// Spec is the metadata for a resource collection.
type Spec struct {
	// Source of the resource that this info is about
	Name Name

	// Proto package name that contains the MessageName type. This is mainly used by code-generation tools.
	ProtoPackage string

	// The MessageName for the proto
	MessageName string
}

// NewSpec returns a new instance of Spec, or error if the input is not valid.
func NewSpec(name, protoPackage, messageName string) (Spec, error) {
	if !IsValidName(name) {
		return Spec{}, fmt.Errorf("invalid collection name: %s", name)
	}

	return Spec{
		Name:         NewName(name),
		ProtoPackage: protoPackage,
		MessageName:  messageName,
	}, nil
}

// MustNewSpec calls NewSpec and panics if it fails.
func MustNewSpec(name, protoPackage, messageName string) Spec {
	s, err := NewSpec(name, protoPackage, messageName)
	if err != nil {
		panic(fmt.Sprintf("MustNewSpec: %v", err))
	}

	return s
}

// Validate the specs. Returns error if there is a problem.
func (s *Spec) Validate() error {
	if getProtoMessageType(s.MessageName) == nil {
		return fmt.Errorf("proto message not found: %v", s.MessageName)
	}
	return nil
}

// String interface method implementation.
func (s *Spec) String() string {
	return fmt.Sprintf("[Spec](%s, %q, %s)", s.Name, s.ProtoPackage, s.MessageName)
}

// NewProtoInstance returns a new instance of the underlying proto for this resource.
func (s *Spec) NewProtoInstance() proto.Message {
	goType := getProtoMessageType(s.MessageName)
	if goType == nil {
		panic(fmt.Errorf("message not found: %q", s.MessageName))
	}

	instance := reflect.New(goType).Interface()

	if p, ok := instance.(proto.Message); !ok {
		panic(fmt.Sprintf(
			"NewProtoInstance: message is not an instance of proto.Message. kind:%s, type:%v, value:%v",
			s.Name, goType, instance))
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
