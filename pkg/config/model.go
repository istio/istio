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

package config

import (
	bytes "bytes"
	"encoding/json"
	"fmt"
	"time"

	gogojsonpb "github.com/gogo/protobuf/jsonpb"
	gogostruct "github.com/gogo/protobuf/types"
	gogotypes "github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	pstruct "github.com/golang/protobuf/ptypes/struct"
)

// Meta is metadata attached to each configuration unit.
// The revision is optional, and if provided, identifies the
// last update operation on the object.
type Meta struct {
	// GroupVersionKind is a short configuration name that matches the content message type
	// (e.g. "route-rule")
	GroupVersionKind GroupVersionKind `json:"type,omitempty"`

	// Name is a unique immutable identifier in a namespace
	Name string `json:"name,omitempty"`

	// Namespace defines the space for names (optional for some types),
	// applications may choose to use namespaces for a variety of purposes
	// (security domains, fault domains, organizational domains)
	Namespace string `json:"namespace,omitempty"`

	// Domain defines the suffix of the fully qualified name past the namespace.
	// Domain is not a part of the unique key unlike name and namespace.
	Domain string `json:"domain,omitempty"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	Annotations map[string]string `json:"annotations,omitempty"`

	// ResourceVersion is an opaque identifier for tracking updates to the config registry.
	// The implementation may use a change index or a commit log for the revision.
	// The config client should not make any assumptions about revisions and rely only on
	// exact equality to implement optimistic concurrency of read-write operations.
	//
	// The lifetime of an object of a particular revision depends on the underlying data store.
	// The data store may compactify old revisions in the interest of storage optimization.
	//
	// An empty revision carries a special meaning that the associated object has
	// not been stored and assigned a revision.
	ResourceVersion string `json:"resourceVersion,omitempty"`

	// CreationTimestamp records the creation time
	CreationTimestamp time.Time `json:"creationTimestamp,omitempty"`
}

// Config is a configuration unit consisting of the type of configuration, the
// key identifier that is unique per type, and the content represented as a
// protobuf message.
type Config struct {
	Meta

	// Spec holds the configuration object as a gogo protobuf message
	Spec ConfigSpec
}

type ConfigSpec interface {
	//	json.Marshaler
	//	json.Unmarshaler
}

func ToProto(s ConfigSpec) (*any.Any, error) {
	if pb, ok := s.(proto.Message); ok {
		return ptypes.MarshalAny(pb)
	}
	js, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	pbs := &pstruct.Struct{}
	if err := jsonpb.Unmarshal(bytes.NewReader(js), pbs); err != nil {
		return nil, err
	}
	return ptypes.MarshalAny(pbs)
}

func ToProtoGogo(s ConfigSpec) (*gogotypes.Any, error) {
	if pb, ok := s.(proto.Message); ok {
		return gogotypes.MarshalAny(pb)
	}
	js, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	pbs := &gogostruct.Struct{}
	if err := gogojsonpb.Unmarshal(bytes.NewReader(js), pbs); err != nil {
		return nil, err
	}
	return gogotypes.MarshalAny(pbs)
}

func ToMap(s ConfigSpec) (map[string]interface{}, error) {
	js, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}

	// Unmarshal from json bytes to go map
	var data map[string]interface{}
	err = json.Unmarshal(js, &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// Key function for the configuration objects
func Key(typ, name, namespace string) string {
	return fmt.Sprintf("%s/%s/%s", typ, namespace, name)
}

// Key is the unique identifier for a configuration object
// TODO: this is *not* unique - needs the version and group
func (meta *Meta) Key() string {
	return Key(meta.GroupVersionKind.Kind, meta.Name, meta.Namespace)
}

func (c Config) DeepCopy() Config {
	var clone Config
	clone.Meta = c.Meta
	if c.Labels != nil {
		clone.Labels = make(map[string]string)
		for k, v := range c.Labels {
			clone.Labels[k] = v
		}
	}
	if c.Annotations != nil {
		clone.Annotations = make(map[string]string)
		for k, v := range c.Annotations {
			clone.Annotations[k] = v
		}
	}
	// TODO!!!!! do not merge
	clone.Spec = c.Spec
	//clone.Spec = proto.Clone(c.Spec)
	return clone
}

var _ fmt.Stringer = GroupVersionKind{}

type GroupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

func (g GroupVersionKind) String() string {
	if g.Group == "" {
		return "core/" + g.Version + "/" + g.Kind
	}
	return g.Group + "/" + g.Version + "/" + g.Kind
}
