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

package kube_test

import (
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/meshconfig"
	kubeMetadata "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube"
	"istio.io/istio/galley/pkg/source/kube/builtin"
	"istio.io/istio/galley/pkg/source/kube/client"
	kubeConverter "istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/testing/events"
	"istio.io/istio/galley/pkg/testing/mock"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/fake"
	dtesting "k8s.io/client-go/testing"
)

var (
	emptyInfo resource.Info

	cfg = &kubeConverter.Config{
		Mesh: meshconfig.NewInMemory(),
	}
)

func init() {
	b := resource.NewSchemaBuilder()
	b.Register("empty", "type.googleapis.com/google.protobuf.Empty")
	s := b.Build()
	emptyInfo, _ = s.Lookup("empty")
}

func TestBuiltin(t *testing.T) {
	g := NewGomegaWithT(t)

	ki := mock.NewKube()
	src := newSource(t, ki, builtin.GetSchema().All()...)
	spec := *builtin.GetSchema().Get("Node")

	// Start the source.
	ch := newChan()
	startOrFail(src, ch, t)
	defer src.Stop()

	// Wait for the full sync event.
	actual := events.ExpectOne(t, ch)
	g.Expect(actual).To(Equal(resource.FullSyncEvent))

	// Create a Node.
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "fakeName",
			ResourceVersion: "rv1",
		},
		Spec: corev1.NodeSpec{
			PodCIDR: "10.40.0.0/24",
		},
	}
	kubeClient, err := ki.KubeClient()
	g.Expect(err).To(BeNil())
	node, err = kubeClient.CoreV1().Nodes().Create(node)
	g.Expect(err).To(BeNil())

	expected := toEvent(resource.Added, spec, node, &node.Spec)
	actual = events.Expect(t, ch)
	g.Expect(actual).To(Equal(expected))
}

func TestDynamic(t *testing.T) {
	g := NewGomegaWithT(t)
	ki := mock.NewKube()
	cl := fake.NewSimpleDynamicClient(k8sRuntime.NewScheme())

	ki.AddResponse(cl, nil)

	i1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "List",
			"metadata": map[string]interface{}{
				"name":            "f1",
				"namespace":       "ns",
				"resourceVersion": "rv1",
			},
			"spec": map[string]interface{}{},
		},
	}

	cl.PrependReactor("*", "foos", func(_ dtesting.Action) (handled bool, ret k8sRuntime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{i1}}, nil
	})
	w := mock.NewWatch()
	cl.PrependWatchReactor("foos", func(_ dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, w, nil
	})

	spec := schema.ResourceSpec{
		Kind:      "List",
		Singular:  "List",
		Plural:    "foos",
		Target:    emptyInfo,
		Converter: kubeConverter.Get("identity"),
	}

	specs := append([]schema.ResourceSpec{spec}, getDynamicSpecs()...)
	ch := make(chan resource.Event)

	s := newSource(t, ki, specs...)
	startOrFail(s, ch, t)
	defer s.Stop()

	expected := resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Key: resource.Key{
					Collection: emptyInfo.Collection,
					FullName:   resource.FullNameFromNamespaceAndName(i1.GetNamespace(), i1.GetName()),
				},
				Version: resource.Version(i1.GetResourceVersion()),
			},
			Metadata: resource.Metadata{
				Labels:      i1.GetLabels(),
				Annotations: i1.GetAnnotations(),
			},
			Item: &types.Empty{},
		},
	}

	actual := events.Expect(t, ch)
	g.Expect(actual).To(Equal(expected))

	// The full sync will come after the event.
	actual = events.ExpectOne(t, ch)
	g.Expect(actual).To(Equal(resource.FullSyncEvent))
}

func newSource(t *testing.T, ki client.Interfaces, specs ...schema.ResourceSpec) runtime.Source {
	src, err := kube.New(ki, 0, schema.New(specs...), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if src == nil {
		t.Fatal("Expected non nil source")
	}
	return src
}

func newChan() chan resource.Event {
	return make(chan resource.Event, 1024)
}

func toEvent(kind resource.EventKind, spec schema.ResourceSpec, objectMeta metav1.Object,
	item proto.Message) resource.Event {
	event := resource.Event{
		Kind: kind,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Key: resource.Key{
					Collection: spec.Target.Collection,
					FullName:   resource.FullNameFromNamespaceAndName(objectMeta.GetNamespace(), objectMeta.GetName()),
				},
				Version: resource.Version(objectMeta.GetResourceVersion()),
			},
			Metadata: resource.Metadata{
				Labels:      objectMeta.GetLabels(),
				Annotations: objectMeta.GetAnnotations(),
			},
			Item: item,
		},
	}

	return event
}

func getDynamicSpecs() []schema.ResourceSpec {
	all := kubeMetadata.Types.All()
	specs := make([]schema.ResourceSpec, 0, len(all))
	for _, spec := range all {
		if !builtin.IsBuiltIn(spec.Kind) {
			specs = append(specs, spec)
		}
	}
	return specs
}

func startOrFail(s runtime.Source, ch chan resource.Event, t *testing.T) {
	t.Helper()
	g := NewGomegaWithT(t)

	err := s.Start(events.ChannelHandler(ch))
	g.Expect(err).To(BeNil())
}
