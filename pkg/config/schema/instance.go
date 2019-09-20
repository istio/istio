// Copyright 2018 Istio Authors
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

package schema

import (
	"fmt"
	"reflect"

	"github.com/gogo/protobuf/proto"
	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v2"

	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

// Instance provides description of the configuration schema and its key function
// nolint: maligned
type Instance struct {
	// ClusterScoped is true for resource in cluster-level.
	ClusterScoped bool

	// VariableName is the name of the generated go variable for this schema. Leave blank to infer from the 'Type' below.
	// This field is used to generate Kube CRD types map (pilot/pkg/config/kube/crd/types.go).
	VariableName string

	// Type is the config proto type.
	Type string

	// Plural is the type in plural.
	Plural string

	// Group is the config proto group.
	Group string

	// Version is the config proto version.
	Version string

	// MessageName refers to the protobuf message type name corresponding to the type
	MessageName string

	// Validate configuration as a protobuf message assuming the object is an
	// instance of the expected message type
	Validate validation.ValidateFunc

	// MCP collection for this configuration resource schema
	Collection string
}

// Make creates a new instance of the proto message
func (i *Instance) Make() (proto.Message, error) {
	pbt := proto.MessageType(i.MessageName)
	if pbt == nil {
		return nil, fmt.Errorf("unknown type %q", i.MessageName)
	}
	return reflect.New(pbt.Elem()).Interface().(proto.Message), nil
}

// FromJSON converts a canonical JSON to a proto message
func (i *Instance) FromJSON(js string) (proto.Message, error) {
	pb, err := i.Make()
	if err != nil {
		return nil, err
	}
	if err = gogoprotomarshal.ApplyJSON(js, pb); err != nil {
		return nil, err
	}
	return pb, nil
}

// FromYAML converts a canonical YAML to a proto message
func (i *Instance) FromYAML(yml string) (proto.Message, error) {
	pb, err := i.Make()
	if err != nil {
		return nil, err
	}
	if err = gogoprotomarshal.ApplyYAML(yml, pb); err != nil {
		return nil, err
	}
	return pb, nil
}

// FromJSONMap converts from a generic map to a proto message using canonical JSON encoding
// JSON encoding is specified here: https://developers.google.com/protocol-buffers/docs/proto3#json
func (i *Instance) FromJSONMap(data interface{}) (proto.Message, error) {
	// Marshal to YAML bytes
	str, err := yaml.Marshal(data)
	if err != nil {
		return nil, err
	}
	out, err := i.FromYAML(string(str))
	if err != nil {
		return nil, multierror.Prefix(err, fmt.Sprintf("YAML decoding error: %v", string(str)))
	}
	return out, nil
}
