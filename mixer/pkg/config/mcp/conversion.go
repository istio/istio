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
	"strings"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
)

var mixerKinds = []string{
	constant.AdapterKind,
	constant.AttributeManifestKind,
	constant.InstanceKind,
	constant.HandlerKind,
	constant.RulesKind,
	constant.TemplateKind,
}

const (
	legacyMixerResourceTypeURL = "type.googleapis.com/istio.mcp.v1alpha1.extensions.LegacyMixerResource"
)

// mapping between Proto Type Urls and and CRD Kinds.
type mapping struct {
	allKnownKinds map[string]struct{}

	typeUrlsToKinds map[string]string
	kindsToTypeUrls map[string]string
}

func constructMapping(kinds []string) *mapping {
	allKnownKinds := make(map[string]struct{})

loop:
	for _, k := range kinds {
		for _, known := range mixerKinds {
			if known == k {
				continue loop
			}
		}
		allKnownKinds[k] = struct{}{}
	}

	m := &mapping{
		allKnownKinds:   allKnownKinds,
		typeUrlsToKinds: make(map[string]string),
		kindsToTypeUrls: make(map[string]string),
	}

	// The mapping is constructed from the common metadata we have for the Kubernetes.
	// Go through Mixer's well-known kinds, and map them to Type URLs.
	kindToUrl := make(map[string]resource.TypeURL)
	for _, spec := range kube.Types.All() {
		kindToUrl[spec.Kind] = spec.Target.TypeURL
	}

	for _, kind := range mixerKinds {
		url, found := kindToUrl[kind]
		if !found {
			scope.Errorf("Type URL for kind not found: %q", kind)
		}
		scope.Infof("Kind -> URL mapping: %17s -> %s", kind, url)

		m.kindsToTypeUrls[kind] = url.String()
		m.typeUrlsToKinds[url.String()] = kind
	}

	return m
}

func (m *mapping) typeUrls() []string {
	var result []string

	for _, url := range m.kindsToTypeUrls {
		result = append(result, url)
	}

	// Add legacyMixerResourceTypeURL as well
	result = append(result, legacyMixerResourceTypeURL)
	return result
}

func (m *mapping) messageNames() []string {
	urls := m.typeUrls()
	result := make([]string, 0, len(urls))

	for _, u := range urls {
		idx := strings.LastIndex(u, "/")
		name := u[idx+1:]
		result = append(result, name)
	}

	return result
}

func (m *mapping) kind(typeURL string) string {
	return m.typeUrlsToKinds[typeURL]
}

func (m *mapping) typeURL(kind string) string {
	return m.kindsToTypeUrls[kind]
}

func (m *mapping) isSupportedLegacyKind(kind string) bool {
	_, found := m.allKnownKinds[kind]
	return found
}

func isLegacyTypeUrl(url string) bool {
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
	// TODO: Ensure that this conversion is correct.
	jsonData, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}

	spec := make(map[string]interface{})
	if err = json.Unmarshal(jsonData, &spec); err != nil {
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
