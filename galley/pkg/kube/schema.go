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

package kube

import (
	"fmt"

	"istio.io/istio/galley/pkg/runtime/resource"
)

// Schema represents a set of known Kubernetes resource types.
type Schema struct {
	entries []ResourceSpec
}

func (e *Schema) add(entry ResourceSpec) {
	e.entries = append(e.entries, entry)
}

// All returns information about all known types.
func (e *Schema) All() []ResourceSpec {
	return e.entries
}

func getTargetFor(typeURL string) resource.Info {
	rInfo, ok := resource.Types.Lookup(typeURL)
	if !ok {
		panic(fmt.Sprintf("Corresponding resource spec not found for: %s", typeURL))
	}
	return rInfo
}
