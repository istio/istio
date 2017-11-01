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
	"strings"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/broker/pkg/model/config"
)

// convertObject translates k8s config JSON to Broker config
func convertObject(schema config.Schema, object IstioObject) (*config.Entry, error) {
	data, err := schema.FromJSONMap(object.GetSpec())
	if err != nil {
		return nil, err
	}
	meta := object.GetObjectMeta()
	return &config.Entry{
		Meta: config.Meta{
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

// convertConfig translates Broker config to k8s config JSON
func convertConfig(schema config.Schema, entry config.Entry) (IstioObject, error) {
	spec, err := schema.ToJSONMap(entry.Spec)
	if err != nil {
		return nil, err
	}
	out := knownTypes[schema.Type].object.DeepCopyObject().(IstioObject)
	out.SetObjectMeta(meta_v1.ObjectMeta{
		Name:            entry.Name,
		Namespace:       entry.Namespace,
		ResourceVersion: entry.ResourceVersion,
		Labels:          entry.Labels,
		Annotations:     entry.Annotations,
	})
	out.SetSpec(spec)

	return out, nil
}

// ResourceName converts "my-name" to "myname".
// This is needed by k8s API server as dashes prevent kubectl from accessing CRDs
func resourceName(s string) string {
	return strings.Replace(s, "-", "", -1)
}

// kabobCaseToCamelCase converts "my-name" to "MyName"
func kabobCaseToCamelCase(s string) string {
	words := strings.Split(s, "-")
	out := ""
	for _, word := range words {
		out = out + strings.Title(word)
	}
	return out
}
