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

package schema

import (
	"fmt"
	"strings"
)

// Mapping between MCP collections and and CRD Kinds.
type Mapping struct {
	// Bidirectional mapping of collections & kinds for non-legacy resources.
	KindsToCollections map[string]string
	CollectionsToKinds map[string]string
}

// ConstructKindMapping constructs a Mapping of kinds to collections. This is mainly used for Mixer v1, and should not
// be depended on generally
func ConstructKindMapping(allKinds []string, metadata *Metadata) (*Mapping, error) {
	// The mapping is constructed from the common metadata we have for the Kubernetes.
	// Go through Mixer's well-known kinds, and map them to collections.

	mixerKindMap := make(map[string]struct{})
	for _, k := range allKinds {
		mixerKindMap[k] = struct{}{}
	}

	// Create a mapping of kind <=> collection for known non-legacy Mixer kinds.
	kindToCollection := make(map[string]string)
	collectionToKind := make(map[string]string)
	for _, r := range metadata.KubeCollections().All() {
		kind := r.Resource().Kind()
		if _, ok := mixerKindMap[kind]; ok {
			source := r.Name()
			target := metadata.DirectTransformSettings().Mapping()[source]
			kindToCollection[kind] = target.String()
			collectionToKind[target.String()] = kind
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

	return &Mapping{
		KindsToCollections: kindToCollection,
		CollectionsToKinds: collectionToKind,
	}, nil
}

// Collections returns all collections that should be requested from the MCP server.
func (m *Mapping) Collections() []string {
	result := make([]string, 0, len(m.CollectionsToKinds)+1)
	for u := range m.CollectionsToKinds {
		result = append(result, u)
	}
	return result
}

// Kind returns the kind for the given collection
func (m *Mapping) Kind(collection string) string {
	return m.CollectionsToKinds[collection]
}
