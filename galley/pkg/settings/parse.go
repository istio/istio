//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package settings

import (
	"bytes"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
)

// Parse the given yaml file into Galley settings.
func Parse(txt string) (*Galley, error) {
	cfg := Default()

	b, err := yaml.JSONToYAML([]byte(txt))
	if err != nil {
		return nil, err
	}

	r := bytes.NewReader(b)
	if err = jsonpb.Unmarshal(r, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// ToYaml serializes Galley settings into Yaml.
func ToYaml(g *Galley) (string, error) {
	m := jsonpb.Marshaler{Indent: "  "}
	js, err := m.MarshalToString(g)
	if err != nil {
		return "", err
	}

	b, err := yaml.JSONToYAML([]byte(js))
	if err != nil {
		return "", err
	}

	return string(b), err
}
