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
	"reflect"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
)

// ToJSON marshals a proto to canonical JSON
func (ps *ProtoSchema) ToJSON(msg proto.Message) (string, error) {
	// Marshal from proto to json bytes
	m := jsonpb.Marshaler{}
	out, err := m.MarshalToString(msg)
	if err != nil {
		return "", err
	}
	return out, nil
}

// ToJSONMap converts a proto message to a generic map using canonical JSON encoding
// JSON encoding is specified here: https://developers.google.com/protocol-buffers/docs/proto3#json
func (ps *ProtoSchema) ToJSONMap(msg proto.Message) (map[string]interface{}, error) {
	js, err := ps.ToJSON(msg)
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
	// Unmarshal from bytes to proto
	pbt := proto.MessageType(ps.MessageName)
	pb := reflect.New(pbt.Elem()).Interface().(proto.Message)
	err := jsonpb.UnmarshalString(js, pb)
	if err != nil {
		return nil, err
	}
	return pb, nil
}

// FromJSONMap converts from a generic map to a proto message using canonical JSON encoding
// JSON encoding is specified here: https://developers.google.com/protocol-buffers/docs/proto3#json
func (ps *ProtoSchema) FromJSONMap(data map[string]interface{}) (proto.Message, error) {
	// Marshal to json bytes
	str, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return ps.FromJSON(string(str))
}
