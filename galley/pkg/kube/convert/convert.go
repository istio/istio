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

// Package convert contains conversion methods between proto.Message and unstructured.Unstructured.
package convert

import (
	"encoding/json"
	"reflect"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/jsonpb"
	yaml2 "gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/galley/pkg/kube/schema"
)

// ToProto reads a proto.Message from the given unstructured data.
func ToProto(t *schema.Type, u *unstructured.Unstructured) (proto.Message, error) {
	return specToProto(t, u.Object["spec"])
}

// ToProtoArray reads an array of proto.Message objects from the given unstructured list.
func ToProtoArray(t *schema.Type, u unstructured.UnstructuredList) ([]proto.Message, error) {
	items := u.Items
	result := make([]proto.Message, len(items))

	for i, item := range items {
		pb, err := specToProto(t, item.Object["spec"])
		if err != nil {
			return nil, err
		}

		result[i] = pb
	}

	return result, nil
}

// ToUnstructured converts the given proto message to an unstructured resource.
func ToUnstructured(t *schema.Type, pb proto.Message) (*unstructured.Unstructured, error) {
	spec, err := protoToSpec(pb)
	if err != nil {
		return nil, err
	}

	u := &unstructured.Unstructured{}
	u.SetKind(t.Kind)
	u.SetAPIVersion(t.GroupVersion().String())
	u.Object["spec"] = spec

	return u, nil
}

func specToProto(t *schema.Type, spec interface{}) (proto.Message, error) {
	js, err := toJSON(spec)
	if err != nil {
		return nil, err
	}

	protoType, err := t.ProtoType()
	if err != nil {
		return nil, err
	}
	pb := reflect.New(protoType.Elem()).Interface().(proto.Message)

	if err = jsonpb.UnmarshalString(js, pb); err != nil {
		return nil, err
	}

	return pb, nil
}

func toJSON(data interface{}) (string, error) {
	yml, err := yaml2.Marshal(data)
	if err != nil {
		return "", err
	}
	js, err := yaml.YAMLToJSON(yml)
	if err != nil {
		return "", err
	}

	return string(js), nil
}

func protoToSpec(pb proto.Message) (map[string]interface{}, error) {
	js, err := protoToJSON(pb)
	if err != nil {
		return nil, err
	}

	// ToProto from json bytes to go map
	var data map[string]interface{}
	err = json.Unmarshal([]byte(js), &data)
	if err != nil {
		return nil, err
	}

	return data, nil

}

// toJSON marshals a proto to canonical JSON
func protoToJSON(msg proto.Message) (string, error) {
	// Marshal from proto to json bytes
	m := jsonpb.Marshaler{}
	out, err := m.MarshalToString(msg)
	if err != nil {
		return "", err
	}
	return out, nil
}
