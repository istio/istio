// Copyright 2019 Istio Authors
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

package pb

import (
	"fmt"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/hashicorp/go-multierror"
	yaml2 "gopkg.in/yaml.v2"

	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

// UnmarshalFromJSON converts a canonical JSON to a proto message
func UnmarshalFromJSON(s collection.Schema, js string) (proto.Message, error) {
	pb, err := s.Resource().NewProtoInstance()
	if err != nil {
		return nil, err
	}
	if err = gogoprotomarshal.ApplyJSON(js, pb); err != nil {
		return nil, err
	}
	return pb, nil
}

// UnmarshalFromYAML converts a canonical YAML to a proto message
func UnmarshalFromYAML(s collection.Schema, yml string) (proto.Message, error) {
	pb, err := s.Resource().NewProtoInstance()
	if err != nil {
		return nil, err
	}
	if err = gogoprotomarshal.ApplyYAML(yml, pb); err != nil {
		return nil, err
	}
	return pb, nil
}

// UnmarshalFromJSONMap converts from a generic map to a proto message using canonical JSON encoding
// JSON encoding is specified here: https://developers.google.com/protocol-buffers/docs/proto3#json
func UnmarshalFromJSONMap(s collection.Schema, data interface{}) (proto.Message, error) {
	// Marshal to YAML bytes
	str, err := yaml.Marshal(data)
	if err != nil {
		return nil, err
	}
	out, err := UnmarshalFromYAML(s, string(str))
	if err != nil {
		return nil, multierror.Prefix(err, fmt.Sprintf("YAML decoding error: %v", string(str)))
	}
	return out, nil
}

// UnmarshalData data into the proto.
func UnmarshalData(pb proto.Message, data interface{}) error {
	js, err := toJSON(data)
	if err == nil {
		u := jsonpb.Unmarshaler{AllowUnknownFields: true}
		err = u.Unmarshal(strings.NewReader(js), pb)
	}
	return err
}

func toJSON(data interface{}) (string, error) {

	var result string
	b, err := yaml2.Marshal(data)
	if err == nil {
		if b, err = yaml.YAMLToJSON(b); err == nil {
			result = string(b)
		}
	}

	return result, err
}
