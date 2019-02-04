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

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/mixer/pkg/config/store"
)

// mapping between MCP collections and and CRD Kinds.
type mapping struct {
	// Bidirectional mapping of collections & kinds for non-legacy resources.
	kindsToCollections map[string]string
	collectionsToKinds map[string]string
}

// construct a mapping of kinds and collections. allKinds is the kind set that was passed
// as part of backend creation.
func constructMapping(allKinds []string, schema *schema.Instance) (*mapping, error) {
	// The mapping is constructed from the common metadata we have for the Kubernetes.
	// Go through Mixer's well-known kinds, and map them to collections.

	mixerKindMap := make(map[string]struct{})
	for _, k := range allKinds {
		mixerKindMap[k] = struct{}{}
	}

	// Create a mapping of kind <=> collection for known non-legacy Mixer kinds.
	kindToCollection := make(map[string]string)
	collectionToKind := make(map[string]string)
	for _, spec := range schema.All() {
		if _, ok := mixerKindMap[spec.Kind]; ok {
			kindToCollection[spec.Kind] = spec.Target.Collection.String()
			collectionToKind[spec.Target.Collection.String()] = spec.Kind
		}
	}

	var missingKinds []string
	for _, mk := range allKinds {
		if _, ok := kindToCollection[mk]; !ok {
			missingKinds = append(missingKinds, mk)
		}
	}
	// We couldn't find metadata for some of the well-known Mixer kinds. This shouldn't happen
	// and is a fatal error.
	if len(missingKinds) > 0 {
		return nil, fmt.Errorf("unable to map some Mixer kinds to collections: %q",
			strings.Join(missingKinds, ","))
	}

	return &mapping{
		kindsToCollections: kindToCollection,
		collectionsToKinds: collectionToKind,
	}, nil
}

// collections returns all collections that should be requested from the MCP server.
func (m *mapping) collections() []string {
	result := make([]string, 0, len(m.collectionsToKinds)+1)
	for u := range m.collectionsToKinds {
		result = append(result, u)
	}
	return result
}

func (m *mapping) kind(collection string) string {
	return m.collectionsToKinds[collection]
}

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

func toBackendResource(key store.Key, labels resource.Labels, annotations resource.Annotations, resource proto.Message,
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
