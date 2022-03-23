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

	"github.com/hashicorp/go-multierror"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/validation"
)

// Schema for a resource.
type Schema interface {
	fmt.Stringer

	// GroupVersionKind of the resource. This is the only way to uniquely identify a resource.
	GroupVersionKind() config.GroupVersionKind

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

	// NewInstance returns a new instance of the protocol buffer message for this resource.
	NewInstance() (config.Spec, error)

	// Status returns the associated status of the schema
	Status() (config.Status, error)

	// StatusKind returns the Kind of the status field. If unset, the field does not support status.
	StatusKind() string
	StatusPackage() string

	// MustNewInstance calls NewInstance and panics if an error occurs.
	MustNewInstance() config.Spec

	// Validate this schema.
	Validate() error

	// ValidateConfig validates that the given config message is of the correct type for this schema
	// and that the contents are valid.
	ValidateConfig(cfg config.Config) (validation.Warning, error)

	// Equal is a helper function for testing equality between Schema instances. This supports comparison
	// with the cmp library.
	Equal(other Schema) bool
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

	StatusProto string

	// ReflectType is the type of the go struct
	ReflectType reflect.Type

	// StatusType is the type of the associated status.
	StatusType reflect.Type

	// ProtoPackage refers to the name of golang package for the protobuf message.
	ProtoPackage string

	// StatusPackage refers to the name of the golang status package.
	StatusPackage string

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
		gvk: config.GroupVersionKind{
			Group:   b.Group,
			Version: b.Version,
			Kind:    b.Kind,
		},
		plural:         b.Plural,
		apiVersion:     b.Group + "/" + b.Version,
		proto:          b.Proto,
		goPackage:      b.ProtoPackage,
		reflectType:    b.ReflectType,
		validateConfig: b.ValidateProto,
		statusType:     b.StatusType,
		statusPackage:  b.StatusPackage,
	}
}

type schemaImpl struct {
	clusterScoped  bool
	gvk            config.GroupVersionKind
	plural         string
	apiVersion     string
	proto          string
	goPackage      string
	validateConfig validation.ValidateFunc
	reflectType    reflect.Type
	statusType     reflect.Type
	statusPackage  string
}

func (s *schemaImpl) GroupVersionKind() config.GroupVersionKind {
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
	return s.goPackage
}

func (s *schemaImpl) StatusPackage() string {
	return s.statusPackage
}

func (s *schemaImpl) Validate() (err error) {
	if !labels.IsDNS1123Label(s.Kind()) {
		err = multierror.Append(err, fmt.Errorf("invalid kind: %s", s.Kind()))
	}
	if !labels.IsDNS1123Label(s.plural) {
		err = multierror.Append(err, fmt.Errorf("invalid plural for kind %s: %s", s.Kind(), s.plural))
	}
	if s.reflectType == nil && getProtoMessageType(s.proto) == nil {
		err = multierror.Append(err, fmt.Errorf("proto message or reflect type not found: %v", s.proto))
	}
	return
}

func (s *schemaImpl) String() string {
	return fmt.Sprintf("[Schema](%s, %q, %s)", s.Kind(), s.goPackage, s.proto)
}

func (s *schemaImpl) NewInstance() (config.Spec, error) {
	rt := s.reflectType
	var instance interface{}
	if rt == nil {
		// Use proto
		t, err := protoMessageType(protoreflect.FullName(s.proto))
		if err != nil || t == nil {
			return nil, errors.New("failed to find reflect type")
		}
		instance = t.New().Interface()
	} else {
		instance = reflect.New(rt).Interface()
	}

	p, ok := instance.(config.Spec)
	if !ok {
		return nil, fmt.Errorf(
			"newInstance: message is not an instance of config.Spec. kind:%s, type:%v, value:%v",
			s.Kind(), rt, instance)
	}
	return p, nil
}

func (s *schemaImpl) Status() (config.Status, error) {
	statTyp := s.statusType
	if statTyp == nil {
		return nil, errors.New("unknown status type")
	}
	instance := reflect.New(statTyp).Interface()
	p, ok := instance.(config.Status)
	if !ok {
		return nil, fmt.Errorf("status: statusType not an instance of config.Status. type: %v, value: %v", statTyp, instance)
	}
	return p, nil
}

func (s *schemaImpl) StatusKind() string {
	if s.statusType == nil {
		return ""
	}
	return s.statusType.Name()
}

func (s *schemaImpl) MustNewInstance() config.Spec {
	p, err := s.NewInstance()
	if err != nil {
		panic(err)
	}
	return p
}

func (s *schemaImpl) ValidateConfig(cfg config.Config) (validation.Warning, error) {
	return s.validateConfig(cfg)
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
func FromKubernetesGVK(in *schema.GroupVersionKind) config.GroupVersionKind {
	return config.GroupVersionKind{
		Group:   in.Group,
		Version: in.Version,
		Kind:    in.Kind,
	}
}

// getProtoMessageType returns the Go lang type of the proto with the specified name.
func getProtoMessageType(protoMessageName string) reflect.Type {
	t, err := protoMessageType(protoreflect.FullName(protoMessageName))
	if err != nil || t == nil {
		return nil
	}
	t.New().Interface()
	return reflect.TypeOf(t.Zero().Interface())
}

var protoMessageType = protoregistry.GlobalTypes.FindMessageByName
