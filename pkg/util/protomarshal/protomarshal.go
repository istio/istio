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

package protomarshal

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/ghodss/yaml"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"istio.io/pkg/log"
)

// ToJSON marshals a proto to canonical JSON
func ToJSON(msg proto.Message) (string, error) {
	if msg == nil {
		return "", errors.New("unexpected nil message")
	}

	// Marshal from proto to json bytes
	b, err := protojson.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ToJSONWithIndent marshals a proto to canonical JSON with pretty printed string
func ToJSONWithIndent(msg proto.Message, indent string) (string, error) {
	if msg == nil {
		return "", errors.New("unexpected nil message")
	}

	// Marshal from proto to json bytes
	b, err := protojson.Marshal(msg)
	if err != nil {
		return "", err
	}
	buf := bytes.Buffer{}
	// protojson is not deterministic, pass through json formatter to add format
	if err := json.Indent(&buf, b, "", indent); err != nil {
		return "", err
	}
	return buf.String(), nil
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

// ApplyJSON unmarshals a JSON string into a proto message.
func ApplyJSON(js string, pb proto.Message) error {
	if err := protojson.Unmarshal([]byte(js), pb); err != nil {
		log.Debugf("Failed to decode proto: %q. Trying decode with AllowUnknownFields=true", err)
		return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal([]byte(js), pb)
	}
	return nil
}

// ApplyJSONStrict unmarshals a JSON string into a proto message.
func ApplyJSONStrict(js string, pb proto.Message) error {
	return protojson.Unmarshal([]byte(js), pb)
}

// ApplyYAML unmarshals a YAML string into a proto message.
// Unknown fields are allowed.
func ApplyYAML(yml string, pb proto.Message) error {
	js, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return err
	}
	return ApplyJSON(string(js), pb)
}
