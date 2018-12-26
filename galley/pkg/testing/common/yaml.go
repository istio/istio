//  Copyright 2018 Istio Authors
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

package common

import (
	"fmt"
	"strings"

	json2 "encoding/json"

	"github.com/ghodss/yaml"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// YamlToUnstructured parses and returns the unmarshaled unstructured.
func YamlToUnstructured(yamlText string) ([]*unstructured.Unstructured, error) {
	chunks := strings.Split(yamlText, "\n---\n")

	var result []*unstructured.Unstructured
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if len(chunk) == 0 {
			continue
		}
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(chunk), u); err != nil {
			return nil, err
		}

		result = append(result, u)
	}

	return result, nil
}

// YamlToResource marshals the given Yaml into resources obtained from the factory
func YamlToResource(yamlText string, factory func(kind string) (interface{}, error)) ([]interface{}, error) {
	us, err := YamlToUnstructured(yamlText)
	if err != nil {
		return nil, err
	}

	var result []interface{}
	for _, u := range us {
		r, err := factory(u.GetKind())
		if err != nil {
			return nil, err
		}
		js, err := u.MarshalJSON()
		if err != nil {
			return nil, err
		}

		if err = json2.Unmarshal(js, r); err != nil {
			return nil, err
		}

		result = append(result, r)
	}

	return result, nil
}

// StandardFactory is a standard factory method for instantiating known-types.
func StandardFactory(kind string) (interface{}, error) {
	switch strings.ToLower(kind) {
	case "ingress":
		return &v1beta1.Ingress{}, nil

	default:
		return nil, fmt.Errorf("unrecognized ingress type: %s", kind)
	}
}
