// Copyright 2017 Istio Authors
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

package model

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/ghodss/yaml"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	multierror "github.com/hashicorp/go-multierror"
	yaml2 "gopkg.in/yaml.v2"
)

// Make creates a new instance of the proto message
func (ps *ProtoSchema) Make() (proto.Message, error) {
	pbt := proto.MessageType(ps.MessageName)
	if pbt == nil {
		return nil, fmt.Errorf("unknown type %q", ps.MessageName)
	}
	return reflect.New(pbt.Elem()).Interface().(proto.Message), nil
}

// ToJSON marshals a proto to canonical JSON
func ToJSON(msg proto.Message) (string, error) {
	// Marshal from proto to json bytes
	m := jsonpb.Marshaler{}
	out, err := m.MarshalToString(msg)
	if err != nil {
		return "", err
	}
	return out, nil
}

// ToYAML marshals a proto to canonical YAML
func ToYAML(msg proto.Message) (string, error) {
	js, err := ToJSON(msg)
	if err != nil {
		return "", err
	}
	yml, err := yaml.JSONToYAML([]byte(js))
	return string(yml), err
}

// ToJSONMap converts a proto message to a generic map using canonical JSON encoding
// JSON encoding is specified here: https://developers.google.com/protocol-buffers/docs/proto3#json
func ToJSONMap(msg proto.Message) (map[string]interface{}, error) {
	js, err := ToJSON(msg)
	if err != nil {
		return nil, err
	}

	// Unmarshal from json bytes to go map
	var data map[string]interface{}
	err = json.Unmarshal([]byte(js), &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// FromJSON converts a canonical JSON to a proto message
func (ps *ProtoSchema) FromJSON(js string) (proto.Message, error) {
	pb, err := ps.Make()
	if err != nil {
		return nil, err
	}
	if err = ApplyJSON(js, pb); err != nil {
		return nil, err
	}
	return pb, nil
}

// ApplyJSON unmarshals a JSON string into a proto message
func ApplyJSON(js string, pb proto.Message) error {
	return jsonpb.UnmarshalString(js, pb)
}

// FromYAML converts a canonical YAML to a proto message
func (ps *ProtoSchema) FromYAML(yml string) (proto.Message, error) {
	pb, err := ps.Make()
	if err != nil {
		return nil, err
	}
	if err = ApplyYAML(yml, pb); err != nil {
		return nil, err
	}
	return pb, nil
}

// ApplyYAML unmarshals a YAML string into a proto message
func ApplyYAML(yml string, pb proto.Message) error {
	js, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return err
	}
	return ApplyJSON(string(js), pb)
}

// FromJSONMap converts from a generic map to a proto message using canonical JSON encoding
// JSON encoding is specified here: https://developers.google.com/protocol-buffers/docs/proto3#json
func (ps *ProtoSchema) FromJSONMap(data interface{}) (proto.Message, error) {
	// Marshal to YAML bytes
	str, err := yaml2.Marshal(data)
	if err != nil {
		return nil, err
	}
	out, err := ps.FromYAML(string(str))
	if err != nil {
		return nil, multierror.Prefix(err, fmt.Sprintf("YAML decoding error: %v", string(str)))
	}
	return out, nil
}

// JSONConfig is the JSON serialized form of the config unit
type JSONConfig struct {
	ConfigMeta

	// Spec is the content of the config
	Spec interface{} `json:"spec,omitempty"`
}

// FromJSON deserializes and validates a JSON config object
func (descriptor ConfigDescriptor) FromJSON(config JSONConfig) (*Config, error) {
	schema, ok := descriptor.GetByType(config.Type)
	if !ok {
		return nil, fmt.Errorf("unknown spec type %s", config.Type)
	}

	message, err := schema.FromJSONMap(config.Spec)
	if err != nil {
		return nil, fmt.Errorf("cannot parse proto message: %v", err)
	}

	if err = schema.Validate(message); err != nil {
		return nil, err
	}
	return &Config{
		ConfigMeta: config.ConfigMeta,
		Spec:       message,
	}, nil
}

// FromYAML deserializes and validates a YAML config object
func (descriptor ConfigDescriptor) FromYAML(content []byte) (*Config, error) {
	out := JSONConfig{}
	err := yaml.Unmarshal(content, &out)
	if err != nil {
		return nil, err
	}
	return descriptor.FromJSON(out)
}

// ToYAML serializes a config into a YAML form
func (descriptor ConfigDescriptor) ToYAML(config Config) (string, error) {
	_, exists := descriptor.GetByType(config.Type)
	if !exists {
		return "", fmt.Errorf("missing type %q", config.Type)
	}

	spec, err := ToJSONMap(config.Spec)
	if err != nil {
		return "", err
	}

	out := JSONConfig{
		ConfigMeta: config.ConfigMeta,
		Spec:       spec,
	}

	bytes, err := yaml.Marshal(out)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}
