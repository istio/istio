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

package collection_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestNames_Clone(t *testing.T) {
	g := NewWithT(t)

	n := collection.Names{collections.All.All()[0].Name(), collections.All.All()[1].Name()}

	n2 := n.Clone()
	g.Expect(n2).To(Equal(n))
}

func TestNames_Sort(t *testing.T) {
	g := NewWithT(t)

	n := collection.Names{collections.IstioMeshV1Alpha1MeshConfig.Name(), collections.IstioExtensionsV1Alpha1Wasmplugins.Name()}
	expected := collection.Names{collections.IstioExtensionsV1Alpha1Wasmplugins.Name(), collections.IstioMeshV1Alpha1MeshConfig.Name()}

	n.Sort()
	g.Expect(n).To(Equal(expected))
}
