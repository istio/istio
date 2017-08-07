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
	"strings"

	"github.com/golang/protobuf/proto"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/pilot/model"
)

// configKey assigns k8s CRD name to Istio config
func configKey(typ, key string) string {
	switch typ {
	case model.RouteRule, model.IngressRule:
		return typ + "-" + key
	case model.DestinationPolicy:
		// TODO: special key encoding for long hostnames-based keys
		parts := strings.Split(key, ".")
		return typ + "-" + strings.Replace(parts[0], "-", "--", -1) +
			"-" + strings.Replace(parts[1], "-", "--", -1)
	}
	return key
}

// modelToKube translates Istio config to k8s config JSON
func modelToKube(schema model.ProtoSchema, namespace string, config proto.Message) (*IstioKind, error) {
	spec, err := schema.ToJSONMap(config)
	if err != nil {
		return nil, err
	}
	out := &IstioKind{
		TypeMeta: meta_v1.TypeMeta{
			Kind: IstioKindName,
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      configKey(schema.Type, schema.Key(config)),
			Namespace: namespace,
		},
		Spec: spec,
	}

	return out, nil
}

// convertConfig extracts Istio config data from k8s CRD
func (cl *Client) convertConfig(item *IstioKind) (model.Config, error) {
	for _, schema := range cl.ConfigDescriptor() {
		if strings.HasPrefix(item.ObjectMeta.Name, schema.Type) {
			data, err := schema.FromJSONMap(item.Spec)
			if err != nil {
				return model.Config{}, err
			}
			return model.Config{
				Type:     schema.Type,
				Key:      schema.Key(data),
				Revision: item.ObjectMeta.ResourceVersion,
				Content:  data,
			}, nil
		}
	}
	return model.Config{}, fmt.Errorf("missing schema")
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
