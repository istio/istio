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

package kuberesource

import (
	"fmt"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
)

func ConvertInputsToSchemas(inputs []config.GroupVersionKind) collection.Schemas {
	resultBuilder := collection.NewSchemasBuilder()
	for _, gv := range inputs {
		s, f := collections.All.FindByGroupVersionKind(gv)
		if !f {
			continue
		}
		_ = resultBuilder.Add(s)
	}

	return resultBuilder.Build()
}

func DefaultExcludedSchemas() collection.Schemas {
	resultBuilder := collection.NewSchemasBuilder()
	for _, r := range collections.Kube.All() {
		if IsDefaultExcluded(r) {
			_ = resultBuilder.Add(r)
		}
	}

	return resultBuilder.Build()
}

// the following code minimally duplicates logic from galley/pkg/config/source/kube/rt/known.go
// without propagating the many dependencies it comes with.

var knownTypes = map[string]struct{}{
	asTypesKey("", "Service"):   {},
	asTypesKey("", "Namespace"): {},
	asTypesKey("", "Node"):      {},
	asTypesKey("", "Pod"):       {},
	asTypesKey("", "Secret"):    {},
}

func asTypesKey(group, kind string) string {
	if group == "" {
		return kind
	}
	return fmt.Sprintf("%s/%s", group, kind)
}

func IsDefaultExcluded(res resource.Schema) bool {
	key := asTypesKey(res.Group(), res.Kind())
	_, ok := knownTypes[key]
	return ok
}
