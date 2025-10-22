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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/log"
)

// FromJSON converts a canonical JSON to a proto message
func FromJSON(s resource.Schema, js string) (config.Spec, error) {
	c, err := s.NewInstance()
	if err != nil {
		return nil, err
	}
	if err = config.ApplyJSON(c, js); err != nil {
		return nil, err
	}
	return c, nil
}

func StatusJSONFromMap(schema resource.Schema, jsonMap *json.RawMessage) (config.Status, error) {
	if jsonMap == nil {
		return nil, nil
	}
	js, err := json.Marshal(jsonMap)
	if err != nil {
		return nil, err
	}
	status, err := schema.Status()
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(js, status)
	if err != nil {
		return nil, err
	}
	return status, nil
}

type ConversionFunc = func(s resource.Schema, js string) (config.Spec, error)

func ConvertObjectInternal(schema resource.Schema, object IstioObject, domain string, convert ConversionFunc) (*config.Config, error) {
	js, err := json.Marshal(object.GetSpec())
	if err != nil {
		return nil, err
	}
	spec, err := convert(schema, string(js))
	if err != nil {
		return nil, err
	}
	status, err := StatusJSONFromMap(schema, object.GetStatus())
	if err != nil {
		log.Errorf("could not get istio status from map %v, err %v", object.GetStatus(), err)
	}
	meta := object.GetObjectMeta()

	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  schema.GroupVersionKind(),
			Name:              meta.Name,
			Namespace:         meta.Namespace,
			Domain:            domain,
			Labels:            meta.Labels,
			Annotations:       meta.Annotations,
			ResourceVersion:   meta.ResourceVersion,
			CreationTimestamp: meta.CreationTimestamp.Time,
		},
		Spec:   spec,
		Status: status,
	}, nil
}

// ConvertObject converts an IstioObject k8s-style object to the internal configuration model.
func ConvertObject(schema resource.Schema, object IstioObject, domain string) (*config.Config, error) {
	return ConvertObjectInternal(schema, object, domain, FromJSON)
}

// ConvertConfig translates Istio config to k8s config JSON
func ConvertConfig(cfg config.Config) (IstioObject, error) {
	spec, err := config.ToRaw(cfg.Spec)
	if err != nil {
		return nil, err
	}
	var status *json.RawMessage
	if cfg.Status != nil {
		s, err := config.ToRaw(cfg.Status)
		if err != nil {
			return nil, err
		}
		// Probably a bit overkill, but this ensures we marshal a pointer to an empty object (&empty{}) as nil
		if !bytes.Equal(s, []byte("{}")) {
			status = &s
		}
	}
	namespace := cfg.Namespace
	if namespace == "" {
		clusterScoped := false
		if s, ok := collections.All.FindByGroupVersionAliasesKind(cfg.GroupVersionKind); ok {
			clusterScoped = s.IsClusterScoped()
		}
		if !clusterScoped {
			namespace = metav1.NamespaceDefault
		}
	}
	return &IstioKind{
		TypeMeta: metav1.TypeMeta{
			Kind:       cfg.GroupVersionKind.Kind,
			APIVersion: cfg.GroupVersionKind.Group + "/" + cfg.GroupVersionKind.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              cfg.Name,
			Namespace:         namespace,
			ResourceVersion:   cfg.ResourceVersion,
			Labels:            cfg.Labels,
			Annotations:       cfg.Annotations,
			CreationTimestamp: metav1.NewTime(cfg.CreationTimestamp),
			UID:               types.UID(cfg.UID),
			Generation:        cfg.Generation,
		},
		Spec:   spec,
		Status: status,
	}, nil
}

// TODO - add special cases for type-to-kind and kind-to-type
// conversions with initial-isms. Consider adding additional type
// information to the abstract model and/or elevating k8s
// representation to first-class type to avoid extra conversions.

func parseInputsImpl(inputs string, withValidate bool) ([]config.Config, []IstioKind, error) {
	var varr []config.Config
	var others []IstioKind
	reader := bytes.NewReader([]byte(inputs))
	empty := IstioKind{}

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
		s, exists := collections.PilotGatewayAPI().FindByGroupVersionAliasesKind(resource.FromKubernetesGVK(&gvk))
		if !exists {
			log.Debugf("unrecognized type %v", obj.Kind)
			others = append(others, obj)
			continue
		}

		cfg, err := ConvertObject(s, &obj, "")
		if err != nil {
			return nil, nil, fmt.Errorf("cannot parse proto message for %v: %v", obj.Name, err)
		}

		if withValidate {
			if _, err := s.ValidateConfig(*cfg); err != nil {
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
// ObjectMeta as identified by the fields in model.Meta. This
// would typically only be a problem if a user dumps an configuration
// object with kubectl and then re-ingests it.
func ParseInputs(inputs string) ([]config.Config, []IstioKind, error) {
	return parseInputsImpl(inputs, true)
}
