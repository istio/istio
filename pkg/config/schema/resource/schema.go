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
	"github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/validation"
)

// Schema for a resource.
type Schema interface {
	fmt.Stringer

	// GroupVersionKind of the resource. This is the only way to uniquely identify a resource.
	GroupVersionKind() GroupVersionKind

	// GroupVersionResource of the resource.
	GroupVersionResource() schema.GroupVersionResource

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

	// APIVersion is a utility that returns a k8s API version string of the form "Group/Version".
	APIVersion() string

	// Proto returns the protocol buffer type name for this resource.
	Proto() string

	// ProtoPackage returns the golang package for the protobuf resource.
	ProtoPackage() string

	// NewProtoInstance returns a new instance of the protocol buffer message for this resource.
	NewProtoInstance() (proto.Message, error)

	// MustNewProtoInstance calls NewProtoInstance and panics if an error occurs.
	MustNewProtoInstance() proto.Message

	// Validate this schema.
	Validate() error

	// ValidateProto validates that the given protocol buffer message is of the correct type for this schema
	// and that the contents are valid.
	ValidateProto(name, namespace string, config proto.Message) error

	// Equal is a helper function for testing equality between Schema instances. This supports comparison
	// with the cmp library.
	Equal(other Schema) bool
}

type GroupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

var _ fmt.Stringer = GroupVersionKind{}

func (g GroupVersionKind) String() string {
	if g.Group == "" {
		return "core/" + g.Version + "/" + g.Kind
	}
	return g.Group + "/" + g.Version + "/" + g.Kind
}

// Builder for a Schema.
type Builder struct {
	// ClusterScoped is true for resource in cluster-level.
	ClusterScoped bool

	// Kind is the config proto type.
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

	return &schemaImpl{
		clusterScoped: b.ClusterScoped,
		gvk: GroupVersionKind{
			Group:   b.Group,
			Version: b.Version,
			Kind:    b.Kind,
		},
		plural:        b.Plural,
		apiVersion:    b.Group + "/" + b.Version,
		proto:         b.Proto,
		protoPackage:  b.ProtoPackage,
		validateProto: b.ValidateProto,
	}
}

type schemaImpl struct {
	clusterScoped bool
	gvk           GroupVersionKind
	plural        string
	apiVersion    string
	proto         string
	protoPackage  string
	validateProto validation.ValidateFunc
}

func (s *schemaImpl) GroupVersionKind() GroupVersionKind {
	return s.gvk
}

func (s *schemaImpl) GroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    s.Group(),
		Version:  s.Version(),
		Resource: s.Plural(),
	}
}

func (s *schemaImpl) IsClusterScoped() bool {
	return s.clusterScoped
}

func (s *schemaImpl) Kind() string {
	return s.gvk.Kind
}

func (s *schemaImpl) Plural() string {
	return s.plural
}

func (s *schemaImpl) Group() string {
	return s.gvk.Group
}

func (s *schemaImpl) Version() string {
	return s.gvk.Version
}

func (s *schemaImpl) APIVersion() string {
	return s.apiVersion
}

func (s *schemaImpl) Proto() string {
	return s.proto
}

func (s *schemaImpl) ProtoPackage() string {
	return s.protoPackage
}

func (s *schemaImpl) Validate() (err error) {
	if !labels.IsDNS1123Label(s.Kind()) {
		err = multierror.Append(err, fmt.Errorf("invalid kind: %s", s.Kind()))
	}
	if !labels.IsDNS1123Label(s.plural) {
		err = multierror.Append(err, fmt.Errorf("invalid plural for kind %s: %s", s.Kind(), s.plural))
	}
	if getProtoMessageType(s.proto) == nil {
		err = multierror.Append(err, fmt.Errorf("proto message not found: %v", s.proto))
	}
	return
}

func (s *schemaImpl) String() string {
	return fmt.Sprintf("[Schema](%s, %q, %s)", s.Kind(), s.protoPackage, s.proto)
}

func (s *schemaImpl) NewProtoInstance() (proto.Message, error) {
	goType := getProtoMessageType(s.proto)
	if goType == nil {
		return nil, fmt.Errorf("message not found: %q", s.proto)
	}

	instance := reflect.New(goType).Interface()

	p, ok := instance.(proto.Message)
	if !ok {
		return nil, fmt.Errorf(
			"newProtoInstance: message is not an instance of proto.Message. kind:%s, type:%v, value:%v",
			s.Kind(), goType, instance)
	}
	return p, nil
}

func (s *schemaImpl) MustNewProtoInstance() proto.Message {
	p, err := s.NewProtoInstance()
	if err != nil {
		panic(err)
	}
	return p
}

func (s *schemaImpl) ValidateProto(name, namespace string, config proto.Message) error {
	return s.validateProto(name, namespace, config)
}

func (s *schemaImpl) Equal(o Schema) bool {
	return s.IsClusterScoped() == o.IsClusterScoped() &&
		s.Kind() == o.Kind() &&
		s.Plural() == o.Plural() &&
		s.Group() == o.Group() &&
		s.Version() == o.Version() &&
		s.Proto() == o.Proto() &&
		s.ProtoPackage() == o.ProtoPackage()
}

// FromKubernetesGVK converts a Kubernetes GVK to an Istio GVK
func FromKubernetesGVK(in *schema.GroupVersionKind) GroupVersionKind {
	return GroupVersionKind{
		Group:   in.Group,
		Version: in.Version,
		Kind:    in.Kind,
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
