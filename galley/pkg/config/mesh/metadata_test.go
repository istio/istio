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

package mesh

import (
	"testing"

	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestMeshConfigNameValidity(t *testing.T) {
	m := schema.MustGet()
	_, found := m.AllCollections().Find(collections.IstioMeshV1Alpha1MeshConfig.Name().String())
	if !found {
		t.Fatalf("Mesh config collection not found in metadata.")
	}
}
