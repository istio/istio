//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package schema

import (
	"fmt"
	"reflect"

	"github.com/gogo/protobuf/proto"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Type represents a known type. It can be used to convert between the K8s and the Proto versions of a type.
type Type struct {
	// Singular name of the K8s resource
	Singular string

	// Plural name of the K8s resource
	Plural string

	// Group name of the K8s resource
	Group string

	// Version of the K8s resource
	Version string

	// Kind of the K8s resource
	Kind string

	// ListKind of the K8s resource
	ListKind string

	// MessageName is the proto message name
	MessageName string
}

// APIResource generated from this type.
func (t *Type) APIResource() *v1.APIResource {
	return &v1.APIResource{
		Name:         t.Plural,
		SingularName: t.Singular,
		Kind:         t.Kind,
		Version:      t.Version,
		Group:        t.Group,
		Namespaced:   true,
	}
}

// GroupVersion returns the GroupVersion of this type.
func (t *Type) GroupVersion() schema.GroupVersion {
	return schema.GroupVersion{
		Group:   t.Group,
		Version: t.Version,
	}
}

// ProtoType returns the Go type of the associated proto message.
func (t *Type) ProtoType() (reflect.Type, error) {
	protoType := proto.MessageType(t.MessageName)
	if protoType == nil {
		return nil, fmt.Errorf("proto message type not found: %q", t.MessageName)
	}

	return protoType, nil
}
