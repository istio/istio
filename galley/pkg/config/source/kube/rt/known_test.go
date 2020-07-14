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
	"fmt"
	"testing"

	"github.com/gogo/protobuf/proto"
	. "github.com/onsi/gomega"
	"k8s.io/api/extensions/v1beta1"

	"istio.io/istio/pkg/config/schema/resource"

	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/k8smeta"
)

func TestParse(t *testing.T) {
	t.Run("Endpoints", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := data.GetEndpoints()

		objMeta, objResource := parse(t, []byte(input), "", "Endpoints", "v1")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.Endpoints)
		if !ok {
			t.Fatal("failed casting item to Endpoints")
		}
		g.Expect(objMeta.GetName()).To(Equal("kube-dns"))
	})

	t.Run("Namespace", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := data.GetNamespace()

		objMeta, objResource := parse(t, []byte(input), "", "Namespace", "v1")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.NamespaceSpec)
		if !ok {
			t.Fatal("failed casting item to Namespace")
		}
		g.Expect(objMeta.GetName()).To(Equal("somens"))
	})

	t.Run("Ingress", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := data.GetIngress()

		objMeta, objResource := parse(t, []byte(input), "extensions", "Ingress", "v1beta1")

		// Just validate a couple of things...
		_, ok := objResource.(*v1beta1.IngressSpec)
		if !ok {
			t.Fatal("failed casting item to IngressSpec")
		}
		g.Expect(objMeta.GetName()).To(Equal("secured-ingress"))
	})

	t.Run("Node", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := data.GetNode()

		objMeta, objResource := parse(t, []byte(input), "", "Node", "v1")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.NodeSpec)
		if !ok {
			t.Fatal("failed casting item to NodeSpec")
		}
		g.Expect(objMeta.GetName()).To(Equal("gke-istio-test-default-pool-866a0405-420r"))
	})

	t.Run("Pod", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := data.GetPod()

		objMeta, objResource := parse(t, []byte(input), "", "Pod", "v1")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.Pod)
		if !ok {
			t.Fatal("failed casting item to Pod")
		}
		g.Expect(objMeta.GetName()).To(Equal("kube-dns-548976df6c-d9kkv"))
	})

	t.Run("Service", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := data.GetService()

		objMeta, objResource := parse(t, []byte(input), "", "Service", "v1")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.ServiceSpec)
		if !ok {
			t.Fatal("failed casting item to ServiceSpec")
		}
		g.Expect(objMeta.GetName()).To(Equal("kube-dns"))
	})

	t.Run("Deployment", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := data.GetDeployment()

		objMeta, objResource := parse(t, []byte(input), "apps", "Deployment", "v1")

		// Just validate a couple of things...
		_, ok := objResource.(*appsV1.Deployment)
		if !ok {
			t.Fatal("failed casting item to Deployment")
		}
		g.Expect(objMeta.GetName()).To(Equal("httpbin"))
	})

}

func TestExtractObject(t *testing.T) {
	for _, r := range k8smeta.MustGet().KubeCollections().All() {
		a := rt.DefaultProvider().GetAdapter(r.Resource())

		t.Run(r.Resource().Kind(), func(t *testing.T) {
			t.Run("WrongTypeShouldReturnNil", func(t *testing.T) {
				out := a.ExtractObject(struct{}{})
				g := NewGomegaWithT(t)
				g.Expect(out).To(BeNil())
			})

			t.Run("Success", func(t *testing.T) {
				out := a.ExtractObject(empty(r.Resource().Kind()))
				g := NewGomegaWithT(t)
				g.Expect(out).ToNot(BeNil())
			})
		})
	}
}

func TestExtractResource(t *testing.T) {
	for _, r := range k8smeta.MustGet().KubeCollections().All() {
		a := rt.DefaultProvider().GetAdapter(r.Resource())

		t.Run(r.Resource().Kind(), func(t *testing.T) {
			t.Run("WrongTypeShouldReturnNil", func(t *testing.T) {
				_, err := a.ExtractResource(struct{}{})
				g := NewGomegaWithT(t)
				g.Expect(err).NotTo(BeNil())
			})

			t.Run("Success", func(t *testing.T) {
				out, err := a.ExtractResource(empty(r.Resource().Kind()))
				g := NewGomegaWithT(t)
				g.Expect(err).To(BeNil())
				g.Expect(out).ToNot(BeNil())
			})
		})
	}
}

func parse(t *testing.T, input []byte, group, kind, version string) (metaV1.Object, proto.Message) {
	t.Helper()
	g := NewGomegaWithT(t)

	pr := rt.DefaultProvider()
	a := pr.GetAdapter(k8smeta.MustGet().KubeCollections().MustFindByGroupVersionKind(resource.GroupVersionKind{
		Group:   group,
		Version: version,
		Kind:    kind,
	}).Resource())
	obj, err := a.ParseJSON(input)
	g.Expect(err).To(BeNil())

	p, err := a.ExtractResource(obj)
	g.Expect(err).To(BeNil())

	return a.ExtractObject(obj), p
}

func empty(kind string) metaV1.Object {
	switch kind {
	case "Node":
		return &coreV1.Node{}
	case "Service":
		return &coreV1.Service{}
	case "Pod":
		return &coreV1.Pod{}
	case "Endpoints":
		return &coreV1.Endpoints{}
	case "Namespace":
		return &coreV1.Namespace{}
	case "Ingress":
		return &v1beta1.Ingress{}
	case "Deployment":
		return &appsV1.Deployment{}
	default:
		panic(fmt.Sprintf("unsupported kind: %v", kind))
	}
}
