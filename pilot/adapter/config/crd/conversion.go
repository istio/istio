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

package crd

import (
	"bytes"
	"strings"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/pilot/model"
)

func convertObject(schema model.ProtoSchema, object IstioObject) (*model.Config, error) {
	data, err := schema.FromJSONMap(object.GetSpec())
	if err != nil {
		return nil, err
	}
	meta := object.GetObjectMeta()
	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:            schema.Type,
			Name:            meta.Name,
			Namespace:       meta.Namespace,
			Labels:          meta.Labels,
			Annotations:     meta.Annotations,
			ResourceVersion: meta.ResourceVersion,
		},
		Spec: data,
	}, nil
}

// convertConfig translates Istio config to k8s config JSON
func convertConfig(schema model.ProtoSchema, config model.Config) (IstioObject, error) {
	spec, err := schema.ToJSONMap(config.Spec)
	if err != nil {
		return nil, err
	}
	out := knownTypes[schema.Type].object.DeepCopyObject().(IstioObject)
	out.SetObjectMeta(meta_v1.ObjectMeta{
		Name:            config.Name,
		Namespace:       config.Namespace,
		ResourceVersion: config.ResourceVersion,
		Labels:          config.Labels,
		Annotations:     config.Annotations,
	})
	out.SetSpec(spec)

	return out, nil
}

// camelCaseToKabobCase converts "my-name" to "MyName"
func kabobCaseToCamelCase(s string) string {
	words := strings.Split(s, "-")
	out := ""
	for _, word := range words {
		out = out + strings.Title(word)
	}
	return out
}

// camelCaseToKabobCase converts "MyName" to "my-name"
// nolint: deadcode
func camelCaseToKabobCase(s string) string {
	var out bytes.Buffer
	for i := range s {
		if 'A' <= s[i] && s[i] <= 'Z' {
			if i > 0 {
				out.WriteByte('-')
			}
			out.WriteByte(s[i] - 'A' + 'a')
		} else {
			out.WriteByte(s[i])
		}
	}
	return out.String()
}
