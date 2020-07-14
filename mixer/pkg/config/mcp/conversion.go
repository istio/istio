//  Copyright Istio Authors
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

package mcp

import (
	"encoding/json"
	"strings"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/config/store"
)

func toKey(kind string, resourceName string) store.Key {
	ns := ""
	localName := resourceName
	if idx := strings.LastIndex(resourceName, "/"); idx != -1 {
		ns = resourceName[:idx]
		localName = resourceName[idx+1:]
	}

	return store.Key{
		Kind:      kind,
		Namespace: ns,
		Name:      localName,
	}
}

func toBackendResource(key store.Key, labels map[string]string, annotations map[string]string, resource proto.Message,
	version string) (*store.BackEndResource, error) {

	marshaller := jsonpb.Marshaler{}
	jsonData, err := marshaller.MarshalToString(resource)
	if err != nil {
		return nil, err
	}

	spec := make(map[string]interface{})
	if err = json.Unmarshal([]byte(jsonData), &spec); err != nil {
		return nil, err
	}

	return &store.BackEndResource{
		Kind: key.Kind,
		Metadata: store.ResourceMeta{
			Name:        key.Name,
			Namespace:   key.Namespace,
			Revision:    version,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: spec,
	}, nil
}
