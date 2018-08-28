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

	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
)

// Well-known non-legacy Mixer types.
var mixerKinds = map[string]struct{}{
	constant.AdapterKind:           {},
	constant.AttributeManifestKind: {},
	constant.InstanceKind:          {},
	constant.HandlerKind:           {},
	constant.RulesKind:             {},
	constant.TemplateKind:          {},
}

const (
	// MessageName, TypeURL for the LegacyMixerResource wrapper type.
	legacyMixerResourceMessageName = "istio.mcp.v1alpha1.extensions.LegacyMixerResource"
	legacyMixerResourceTypeURL     = "type.googleapis.com/" + legacyMixerResourceMessageName
)

// mapping between Proto Type Urls and and CRD Kinds.
type mapping struct {
	// The set of legacy Mixer resource kinds.
	legacyKinds map[string]struct{}

	// Bidirectional mapping of type URL & kinds for non-legacy resources.
	kindsToTypeURLs map[string]string
	typeURLsToKinds map[string]string
}

// construct a mapping of kinds and TypeURLs. allKinds is the kind set that was passed
// as part of backend creation.
func constructMapping(allKinds []string, schema *kube.Schema) (*mapping, error) {

	// Calculate the legacy kinds.
	legacyKinds := make(map[string]struct{})
	for _, k := range allKinds {
		if _, ok := mixerKinds[k]; !ok {
			legacyKinds[k] = struct{}{}
		}
	}

	// The mapping is constructed from the common metadata we have for the Kubernetes.
	// Go through Mixer's well-known kinds, and map them to Type URLs.

	// Create a mapping of kind <=> TypeURL for known non-legacy Mixer kinds.
	kindToURL := make(map[string]string)
	urlToKind := make(map[string]string)
	for _, spec := range schema.All() {
		if _, ok := mixerKinds[spec.Kind]; ok {
			kindToURL[spec.Kind] = spec.Target.TypeURL.String()
			urlToKind[spec.Target.TypeURL.String()] = spec.Kind
		}
	}

	if len(mixerKinds) != len(kindToURL) {
		// We couldn't find metadata for some of the well-known Mixer kinds. This shouldn't happen
		// and is a fatal error.
		var problemKinds []string
		for mk := range mixerKinds {
			if _, ok := kindToURL[mk]; !ok {
				problemKinds = append(problemKinds, mk)
			}
		}

		return nil, fmt.Errorf("unable to map some Mixer kinds to TypeURLs: %q",
			strings.Join(problemKinds, ","))
	}

	return &mapping{
		legacyKinds:     legacyKinds,
		kindsToTypeURLs: kindToURL,
		typeURLsToKinds: urlToKind,
	}, nil
}

// typeURLs returns all TypeURLs that should be requested from the MCP server.
func (m *mapping) typeURLs() []string {
	result := make([]string, 0, len(m.typeURLsToKinds)+1)
	for u := range m.typeURLsToKinds {
		result = append(result, u)
	}
	result = append(result, legacyMixerResourceTypeURL)

	return result
}

func (m *mapping) kind(typeURL string) string {
	return m.typeURLsToKinds[typeURL]
}

func isLegacyTypeURL(url string) bool {
	return url == legacyMixerResourceTypeURL
}

func toKey(kind string, resourceName string) store.Key {
	// TODO: This is a dependency on the name format. For the short term, we will parse the resource name,
	// assuming it is in the ns/name format. For the long term, we should update the code to stop it from
	// depending on namespaces.
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

func toBackendResource(key store.Key, resource proto.Message, version string) (*store.BackEndResource, error) {
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
			Name:      key.Name,
			Namespace: key.Namespace,
			Revision:  version,
		},
		Spec: spec,
	}, nil
}
