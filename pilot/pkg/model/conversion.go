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
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
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
	return ToJSONWithIndent(msg, "")
}

// ToJSONWithIndent marshals a proto to canonical JSON with pretty printed string
func ToJSONWithIndent(msg proto.Message, indent string) (string, error) {
	if msg == nil {
		return "", errors.New("unexpected nil message")
	}

	// Marshal from proto to json bytes
	m := jsonpb.Marshaler{Indent: indent}
	return m.MarshalToString(msg)
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
	if err = ApplyJSON(js, pb, true); err != nil {
		return nil, err
	}
	return pb, nil
}

// ApplyJSON unmarshals a JSON string into a proto message. Unknown fields will produce an
//// error unless strict is set to false.
func ApplyJSON(js string, pb proto.Message, strict bool) error {
	reader := strings.NewReader(js)
	m := jsonpb.Unmarshaler{}
	if err := m.Unmarshal(reader, pb); err != nil {
		if strict {
			return err
		}

		log.Warnf("Failed to decode proto: %q. Trying decode with AllowUnknownFields=true", err)
		m.AllowUnknownFields = true
		reader.Reset(js)
		return m.Unmarshal(reader, pb)
	}
	return nil
}

// FromYAML converts a canonical YAML to a proto message
func (ps *ProtoSchema) FromYAML(yml string) (proto.Message, error) {
	pb, err := ps.Make()
	if err != nil {
		return nil, err
	}
	if err = ApplyYAML(yml, pb, true); err != nil {
		return nil, err
	}
	return pb, nil
}

// ApplyYAML unmarshals a YAML string into a proto message. Unknown fields will produce an
// error unless strict is set to false.
func ApplyYAML(yml string, pb proto.Message, strict bool) error {
	js, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return err
	}
	return ApplyJSON(string(js), pb, strict)
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
