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

package rt_test

import (
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/istio/pkg/config/schema/resource"

	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/data"
)

func TestParseDynamic(t *testing.T) {
	g := NewGomegaWithT(t)
	input, err := yaml.ToJSON([]byte(data.YamlN1I1V1))
	g.Expect(err).To(BeNil())
	objMeta, objResource := parseDynamic(t, input, "Kind1")

	// Just validate a couple of things...
	_, ok := objResource.(*types.Struct)
	if !ok {
		t.Fatal("failed casting item to Endpoints")
	}
	g.Expect(objMeta.GetNamespace()).To(Equal("n1"))
	g.Expect(objMeta.GetName()).To(Equal("i1"))
}

func TestExtractObjectDynamic(t *testing.T) {
	for _, r := range basicmeta.MustGet().KubeCollections().All() {
		a := rt.DefaultProvider().GetAdapter(r.Resource())

		t.Run(r.Resource().Kind(), func(t *testing.T) {
			t.Run("WrongTypeShouldReturnNil", func(t *testing.T) {
				out := a.ExtractObject(struct{}{})
				g := NewGomegaWithT(t)
				g.Expect(out).To(BeNil())
			})

			t.Run("Success", func(t *testing.T) {
				out := a.ExtractObject(&unstructured.Unstructured{})
				g := NewGomegaWithT(t)
				g.Expect(out).ToNot(BeNil())
			})
		})
	}
}

func TestExtractResourceDynamic(t *testing.T) {
	for _, r := range basicmeta.MustGet().KubeCollections().All() {
		a := rt.DefaultProvider().GetAdapter(r.Resource())

		t.Run(r.Resource().Kind(), func(t *testing.T) {
			t.Run("WrongTypeShouldReturnNil", func(t *testing.T) {
				_, err := a.ExtractResource(struct{}{})
				g := NewGomegaWithT(t)
				g.Expect(err).NotTo(BeNil())
			})

			t.Run("Success", func(t *testing.T) {
				out, err := a.ExtractResource(&unstructured.Unstructured{})
				g := NewGomegaWithT(t)
				g.Expect(err).To(BeNil())
				g.Expect(out).ToNot(BeNil())
			})
		})
	}
}

func parseDynamic(t *testing.T, input []byte, kind string) (metaV1.Object, proto.Message) {
	t.Helper()
	g := NewGomegaWithT(t)

	pr := rt.DefaultProvider()
	a := pr.GetAdapter(basicmeta.MustGet().KubeCollections().MustFindByGroupVersionKind(resource.GroupVersionKind{
		Group:   "testdata.istio.io",
		Version: "v1alpha1",
		Kind:    kind,
	}).Resource())

	obj, err := a.ParseJSON(input)
	g.Expect(err).To(BeNil())

	p, err := a.ExtractResource(obj)
	g.Expect(err).To(BeNil())

	return a.ExtractObject(obj), p
}
