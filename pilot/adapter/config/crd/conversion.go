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

	"github.com/golang/protobuf/proto"

	"istio.io/pilot/model"
)

// configKey assigns k8s CRD name to Istio config
func configKey(typ, key string) string {
	switch typ {
	case model.RouteRule.Type, model.IngressRule.Type:
		return key
	case model.DestinationPolicy.Type:
		// TODO: special key encoding for long hostnames-based keys
		parts := strings.Split(key, ".")
		return strings.Replace(parts[0], "-", "--", -1) +
			"-" + strings.Replace(parts[1], "-", "--", -1)
	}
	return key
}

// modelToKube translates Istio config to k8s config JSON
func modelToKube(schema model.ProtoSchema, namespace string, config proto.Message,
	revision string) (IstioObject, error) {
	spec, err := schema.ToJSONMap(config)
	if err != nil {
		return nil, err
	}
	out := knownTypes[schema.Type].object.DeepCopyObject().(IstioObject)
	out.SetObjectMeta(meta_v1.ObjectMeta{
		Name:            configKey(schema.Type, schema.Key(config)),
		Namespace:       namespace,
		ResourceVersion: revision,
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
