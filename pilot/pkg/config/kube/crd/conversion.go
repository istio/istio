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

package crd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"github.com/gogo/protobuf/proto"
	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v2"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/istio/pkg/config/schema/resource"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

// FromJSON converts a canonical JSON to a proto message
func FromJSON(s collection.Schema, js string) (proto.Message, error) {
	pb, err := s.Resource().NewProtoInstance()
	if err != nil {
		return nil, err
	}
	if err = gogoprotomarshal.ApplyJSON(js, pb); err != nil {
		return nil, err
	}
	return pb, nil
}

// FromYAML converts a canonical YAML to a proto message
func FromYAML(s collection.Schema, yml string) (proto.Message, error) {
	pb, err := s.Resource().NewProtoInstance()
	if err != nil {
		return nil, err
	}
	if err = gogoprotomarshal.ApplyYAML(yml, pb); err != nil {
		return nil, err
	}
	return pb, nil
}

// FromJSONMap converts from a generic map to a proto message using canonical JSON encoding
// JSON encoding is specified here: https://developers.google.com/protocol-buffers/docs/proto3#json
func FromJSONMap(s collection.Schema, data interface{}) (proto.Message, error) {
	// Marshal to YAML bytes
	str, err := yaml.Marshal(data)
	if err != nil {
		return nil, err
	}
	out, err := FromYAML(s, string(str))
	if err != nil {
		return nil, multierror.Prefix(err, fmt.Sprintf("YAML decoding error: %v", string(str)))
	}
	return out, nil
}

// ConvertObject converts an IstioObject k8s-style object to the internal configuration model.
func ConvertObject(schema collection.Schema, object IstioObject, domain string) (*model.Config, error) {
	js, err := json.Marshal(object.GetSpec())
	if err != nil {
		return nil, err
	}
	data, err := FromJSON(schema, string(js))
	if err != nil {
		return nil, err
	}
	meta := object.GetObjectMeta()

	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind:  schema.Resource().GroupVersionKind(),
			Name:              meta.Name,
			Namespace:         meta.Namespace,
			Domain:            domain,
			Labels:            meta.Labels,
			Annotations:       meta.Annotations,
			ResourceVersion:   meta.ResourceVersion,
			CreationTimestamp: meta.CreationTimestamp.Time,
		},
		Spec: data,
	}, nil
}

// ConvertConfig translates Istio config to k8s config JSON
func ConvertConfig(cfg model.Config) (IstioObject, error) {
	spec, err := gogoprotomarshal.ToJSONMap(cfg.Spec)
	if err != nil {
		return nil, err
	}
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = meta_v1.NamespaceDefault
	}
	return &IstioKind{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       cfg.GroupVersionKind.Kind,
			APIVersion: cfg.GroupVersionKind.Group + "/" + cfg.GroupVersionKind.Version,
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name:              cfg.Name,
			Namespace:         namespace,
			ResourceVersion:   cfg.ResourceVersion,
			Labels:            cfg.Labels,
			Annotations:       cfg.Annotations,
			CreationTimestamp: meta_v1.NewTime(cfg.CreationTimestamp),
		},
		Spec: spec,
	}, nil
}

// TODO - add special cases for type-to-kind and kind-to-type
// conversions with initial-isms. Consider adding additional type
// information to the abstract model and/or elevating k8s
// representation to first-class type to avoid extra conversions.

func parseInputsImpl(inputs string, withValidate bool) ([]model.Config, []IstioKind, error) {
	var varr []model.Config
	var others []IstioKind
	reader := bytes.NewReader([]byte(inputs))
	var empty = IstioKind{}

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
		if reflect.DeepEqual(obj, empty) {
			continue
		}

		gvk := obj.GroupVersionKind()
		s, exists := collections.PilotServiceApi.FindByGroupVersionKind(resource.FromKubernetesGVK(&gvk))
		if !exists {
			log.Debugf("unrecognized type %v", obj.Kind)
			others = append(others, obj)
			continue
		}

		cfg, err := ConvertObject(s, &obj, "")
		if err != nil {
			return nil, nil, fmt.Errorf("cannot parse proto message: %v", err)
		}

		if withValidate {
			if err := s.Resource().ValidateProto(cfg.Name, cfg.Namespace, cfg.Spec); err != nil {
				return nil, nil, fmt.Errorf("configuration is invalid: %v", err)
			}
		}

		varr = append(varr, *cfg)
	}

	return varr, others, nil
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
	return parseInputsImpl(inputs, true)
}

// ParseInputsWithoutValidation same as ParseInputs, but do not apply schema validation.
func ParseInputsWithoutValidation(inputs string) ([]model.Config, []IstioKind, error) {
	return parseInputsImpl(inputs, false)
}
