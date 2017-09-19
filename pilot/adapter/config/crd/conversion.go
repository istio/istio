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
	"fmt"
	"io"
	"strings"

	"github.com/golang/glog"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/pilot/model"
)

// ConvertObject converts an IstioObject k8s-style object to the
// internal configuration model.
func ConvertObject(schema model.ProtoSchema, object IstioObject, domain string) (*model.Config, error) {
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
			Domain:          domain,
			Labels:          meta.Labels,
			Annotations:     meta.Annotations,
			ResourceVersion: meta.ResourceVersion,
		},
		Spec: data,
	}, nil
}

// ConvertConfig translates Istio config to k8s config JSON
func ConvertConfig(schema model.ProtoSchema, config model.Config) (IstioObject, error) {
	spec, err := model.ToJSONMap(config.Spec)
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

// ResourceName converts "my-name" to "myname".
// This is needed by k8s API server as dashes prevent kubectl from accessing CRDs
func ResourceName(s string) string {
	return strings.Replace(s, "-", "", -1)
}

// KabobCaseToCamelCase converts "my-name" to "MyName"
func KabobCaseToCamelCase(s string) string {
	words := strings.Split(s, "-")
	out := ""
	for _, word := range words {
		out = out + strings.Title(word)
	}
	return out
}

// CamelCaseToKabobCase converts "MyName" to "my-name"
func CamelCaseToKabobCase(s string) string {
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

// ParseInputs reads multiple documents from `kubectl` output and checks with
// the schema. It also returns the list of unrecognized kinds as the second
// response.
//
// NOTE: This function only decodes a subset of the complete k8s
// ObjectMeta as identified by the fields in model.ConfigMeta. This
// would typically only be a problem if a user dumps an configuration
// object with kubectl and then re-ingests it.
func ParseInputs(inputs string) ([]model.Config, []IstioKind, error) {
	var varr []model.Config
	var others []IstioKind
	reader := bytes.NewReader([]byte(inputs))

	// We store configs as a YaML stream; there may be more than one decoder.
	yamlDecoder := kubeyaml.NewYAMLOrJSONDecoder(reader, 512*1024)
	for {
		obj := IstioKind{}
		err := yamlDecoder.Decode(&obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("cannot parse proto message: %v", err)
		}

		schema, exists := model.IstioConfigTypes.GetByType(CamelCaseToKabobCase(obj.Kind))
		if !exists {
			glog.V(7).Infof("unrecognized type %v", obj.Kind)
			others = append(others, obj)
			continue
		}

		config, err := ConvertObject(schema, &obj, "")
		if err != nil {
			return nil, nil, fmt.Errorf("cannot parse proto message: %v", err)
		}

		if err := schema.Validate(config.Spec); err != nil {
			return nil, nil, fmt.Errorf("configuration is invalid: %v", err)
		}

		varr = append(varr, *config)
	}

	return varr, others, nil
}
