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
	"errors"
	"fmt"
	"reflect"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/config/validation"
)

// Schema for a resource.
type Schema interface {
	fmt.Stringer

	// CanonicalName of the resource.
	CanonicalName() string

	// IsClusterScoped indicates that this resource is scoped to a particular namespace within a cluster.
	IsClusterScoped() bool

	// Kind for this resource.
	Kind() string

	// Plural returns the plural form of the Kind.
	Plural() string

	// Group for this resource.
	Group() string

	// Version of this resource.
	Version() string

	// Proto returns the protocol buffer type name for this resource.
	Proto() string

	// ProtoPackage returns the golang package for the protobuf resource.
	ProtoPackage() string

	// NewProtoInstance returns a new instance of the protocol buffer message for this resource.
	NewProtoInstance() proto.Message

	// Validate this schema.
	Validate() error

	// ValidateProto validates that the given protocol buffer message is of the correct type for this schema
	// and that the contents are valid.
	ValidateProto(name, namespace string, config proto.Message) error

	// Equal is a helper function for testing equality between Schema instances. This supports comparison
	// with the cmp library.
	Equal(other Schema) bool
}

// Builder for a Schema.
type Builder struct {
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

// Build a Schema instance.
func (b Builder) Build() (Schema, error) {
	s := b.BuildNoValidate()

	// Validate the schema.
	if err := s.Validate(); err != nil {
		return nil, err
	}

	return s, nil
}

// MustBuild calls Build and panics if it fails.
func (b Builder) MustBuild() Schema {
	s, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("MustBuild: %v", err))
	}
	return s
}

// BuildNoValidate builds the Schema without checking the fields.
func (b Builder) BuildNoValidate() Schema {
	if b.ValidateProto == nil {
		b.ValidateProto = validation.EmptyValidate
	}

	return &immutableSchema{
		clusterScoped: b.ClusterScoped,
		kind:          b.Kind,
		plural:        b.Plural,
		group:         b.Group,
		version:       b.Version,
		proto:         b.Proto,
		protoPackage:  b.ProtoPackage,
		validateProto: b.ValidateProto,
	}
}

type immutableSchema struct {
	clusterScoped bool
	kind          string
	plural        string
	group         string
	version       string
	proto         string
	protoPackage  string
	validateProto validation.ValidateFunc
}

func (s *immutableSchema) IsClusterScoped() bool {
	return s.clusterScoped
}

func (s *immutableSchema) Kind() string {
	return s.kind
}

func (s *immutableSchema) Plural() string {
	return s.plural
}

func (s *immutableSchema) Group() string {
	return s.group
}

func (s *immutableSchema) Version() string {
	return s.version
}

func (s *immutableSchema) Proto() string {
	return s.proto
}

func (s *immutableSchema) ProtoPackage() string {
	return s.protoPackage
}

func (s *immutableSchema) CanonicalName() string {
	if s.group == "" {
		return "core/" + s.version + "/" + s.kind
	}
	return s.group + "/" + s.version + "/" + s.kind
}

func (s *immutableSchema) Validate() error {
	if s.kind == "" {
		return errors.New("kind must be specified")
	}
	if getProtoMessageType(s.proto) == nil {
		return fmt.Errorf("proto message not found: %v", s.proto)
	}
	return nil
}

// String interface method implementation.
func (s *immutableSchema) String() string {
	return fmt.Sprintf("[Schema](%s, %q, %s)", s.kind, s.protoPackage, s.proto)
}

// NewProtoInstance returns a new instance of the underlying proto for this resource.
func (s *immutableSchema) NewProtoInstance() proto.Message {
	goType := getProtoMessageType(s.proto)
	if goType == nil {
		panic(fmt.Errorf("message not found: %q", s.proto))
	}

	instance := reflect.New(goType).Interface()

	if p, ok := instance.(proto.Message); !ok {
		panic(fmt.Sprintf(
			"NewProtoInstance: message is not an instance of proto.Message. kind:%s, type:%v, value:%v",
			s.kind, goType, instance))
	} else {
		return p
	}
}

func (s *immutableSchema) ValidateProto(name, namespace string, config proto.Message) error {
	return s.validateProto(name, namespace, config)
}

func (s *immutableSchema) Equal(o Schema) bool {
	return s.IsClusterScoped() == o.IsClusterScoped() &&
		s.Kind() == o.Kind() &&
		s.Plural() == o.Plural() &&
		s.Group() == o.Group() &&
		s.Version() == o.Version() &&
		s.Proto() == o.Proto() &&
		s.ProtoPackage() == o.ProtoPackage()
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
