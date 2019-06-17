// Copyright 2019 Istio Authors
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

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/data/builtin"
	"istio.io/istio/galley/pkg/config/testing/k8smeta"
)

func TestParse(t *testing.T) {
	t.Run("Endpoints", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := builtin.GetEndpoints()

		objMeta, objResource := parse(t, []byte(input), "Endpoints")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.Endpoints)
		if !ok {
			t.Fatal("failed casting item to Endpoints")
		}
		g.Expect(objMeta.GetName()).To(Equal("kube-dns"))
	})

	t.Run("Node", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := builtin.GetNode()

		objMeta, objResource := parse(t, []byte(input), "Node")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.NodeSpec)
		if !ok {
			t.Fatal("failed casting item to NodeSpec")
		}
		g.Expect(objMeta.GetName()).To(Equal("gke-istio-test-default-pool-866a0405-420r"))
	})

	t.Run("Pod", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := builtin.GetPod()

		objMeta, objResource := parse(t, []byte(input), "Pod")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.Pod)
		if !ok {
			t.Fatal("failed casting item to Pod")
		}
		g.Expect(objMeta.GetName()).To(Equal("kube-dns-548976df6c-d9kkv"))
	})

	t.Run("Service", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := builtin.GetService()

		objMeta, objResource := parse(t, []byte(input), "Service")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.ServiceSpec)
		if !ok {
			t.Fatal("failed casting item to ServiceSpec")
		}
		g.Expect(objMeta.GetName()).To(Equal("kube-dns"))
	})
}

func TestExtractObject(t *testing.T) {
	for _, r := range k8smeta.MustGet().KubeSource().Resources() {
		a := rt.DefaultProvider().GetAdapter(r)

		t.Run(r.Kind, func(t *testing.T) {
			t.Run("WrongTypeShouldReturnNil", func(t *testing.T) {
				out := a.ExtractObject(struct{}{})
				g := NewGomegaWithT(t)
				g.Expect(out).To(BeNil())
			})

			t.Run("Success", func(t *testing.T) {
				out := a.ExtractObject(empty(r.Kind))
				g := NewGomegaWithT(t)
				g.Expect(out).ToNot(BeNil())
			})
		})
	}
}

func TestExtractResource(t *testing.T) {
	for _, r := range k8smeta.MustGet().KubeSource().Resources() {
		a := rt.DefaultProvider().GetAdapter(r)

		t.Run(r.Kind, func(t *testing.T) {
			t.Run("WrongTypeShouldReturnNil", func(t *testing.T) {
				_, err := a.ExtractResource(struct{}{})
				g := NewGomegaWithT(t)
				g.Expect(err).NotTo(BeNil())
			})

			t.Run("Success", func(t *testing.T) {
				out, err := a.ExtractResource(empty(r.Kind))
				g := NewGomegaWithT(t)
				g.Expect(err).To(BeNil())
				g.Expect(out).ToNot(BeNil())
			})
		})
	}
}

func parse(t *testing.T, input []byte, kind string) (metaV1.Object, proto.Message) {
	t.Helper()
	g := NewGomegaWithT(t)

	pr := rt.DefaultProvider()
	a := pr.GetAdapter(k8smeta.MustGet().KubeSource().Resources().MustFind("", kind))
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
	default:
		panic(fmt.Sprintf("unsupported kind: %v", kind))
	}
}
