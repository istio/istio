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

package builtin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/builtin"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	kubeLog "istio.io/istio/galley/pkg/source/kube/log"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/testing/events"
	"istio.io/istio/pkg/log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	name      = "fakeResource"
	namespace = "fakeNamespace"
)

var (
	fakeCreateTime, _ = time.Parse(time.RFC3339, "2009-02-04T21:00:57-08:00")
	fakeObjectMeta    = metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		CreationTimestamp: metav1.Time{
			Time: fakeCreateTime,
		},
		Labels: map[string]string{
			"lk1": "lv1",
		},
		Annotations: map[string]string{
			"ak1": "av1",
		},
		ResourceVersion: "rv1",
	}
)

func TestNewWithUnknownSpecShouldError(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	client := fake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)

	spec := schema.ResourceSpec{
		Kind:      "Unknown",
		ListKind:  "UnknownList",
		Singular:  "unknown",
		Plural:    "unknowns",
		Version:   "v1alpha1",
		Group:     "cofig.istio.io",
		Converter: converter.Get("identity"),
	}
	_, err := builtin.New(informerFactory, spec)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("Expected error not found: %v", err)
	}
}

func TestStartTwiceShouldError(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	g := NewGomegaWithT(t)

	// Start the source.
	_, informerFactory := kubeResources()
	spec := builtin.GetType("Node").GetSpec()
	ch := make(chan resource.Event)
	s := newOrFail(t, informerFactory, spec)
	_ = startOrFail(t, s)
	defer s.Stop()

	// Start again - should fail.
	err := s.Start(events.ChannelHandler(ch))
	g.Expect(err).ToNot(BeNil())
}

func TestStopTwiceShouldSucceed(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	// Start the source.
	_, informerFactory := kubeResources()
	spec := builtin.GetType("Node").GetSpec()
	s := newOrFail(t, informerFactory, spec)
	_ = startOrFail(t, s)

	// Stop the resource twice.
	s.Stop()
	s.Stop()
}

func TestUnknownResourceShouldNotCreateEvent(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	client, informerFactory := kubeResources()
	spec := builtin.GetType("Node").GetSpec()

	// Start the source.
	s := newOrFail(t, informerFactory, spec)
	ch := startOrFail(t, s)
	defer s.Stop()

	expectFullSync(t, ch)

	node := &corev1.Node{
		ObjectMeta: fakeObjectMeta,
		Spec: corev1.NodeSpec{
			PodCIDR: "10.40.0.0/24",
		},
	}
	node.Namespace = "" // nodes don't have namespaces.

	// Add the resource.
	g := NewGomegaWithT(t)
	var err error
	if node, err = client.CoreV1().Nodes().Create(node); err != nil {
		t.Fatalf("failed creating node: %v", err)
	}
	expected := toEvent(resource.Added, spec, node, &node.Spec)
	actual := events.ExpectOne(t, ch)
	g.Expect(actual).To(Equal(expected))
}

func TestNodes(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	client, informerFactory := kubeResources()
	spec := builtin.GetType("Node").GetSpec()

	// Start the source.
	s := newOrFail(t, informerFactory, spec)
	ch := startOrFail(t, s)
	defer s.Stop()

	expectFullSync(t, ch)

	node := &corev1.Node{
		ObjectMeta: fakeObjectMeta,
		Spec: corev1.NodeSpec{
			PodCIDR: "10.40.0.0/24",
		},
	}
	node.Namespace = "" // nodes don't have namespaces.

	// Add the resource.
	t.Run("Add", func(t *testing.T) {
		g := NewGomegaWithT(t)
		var err error
		if node, err = client.CoreV1().Nodes().Create(node); err != nil {
			t.Fatalf("failed creating node: %v", err)
		}
		expected := toEvent(resource.Added, spec, node, &node.Spec)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	// Update the resource.
	t.Run("Update", func(t *testing.T) {
		g := NewGomegaWithT(t)
		node.Spec.PodCIDR = "10.20.0.0/32"
		node.ResourceVersion = "rv2"
		if _, err := client.CoreV1().Nodes().Update(node); err != nil {
			t.Fatalf("failed updating node: %v", err)
		}
		expected := toEvent(resource.Updated, spec, node, &node.Spec)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	// Update event with no changes, should yield no events.
	t.Run("UpdateNoChange", func(t *testing.T) {
		if _, err := client.CoreV1().Nodes().Update(node); err != nil {
			t.Fatalf("failed updating node: %v", err)
		}
		events.ExpectNone(t, ch)
	})

	// Delete the resource.
	t.Run("Delete", func(t *testing.T) {
		g := NewGomegaWithT(t)
		if err := client.CoreV1().Nodes().Delete(node.Name, nil); err != nil {
			t.Fatalf("failed deleting node: %v", err)
		}
		expected := toEvent(resource.Deleted, spec, node, &node.Spec)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})
}

func TestPods(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	client, informerFactory := kubeResources()

	spec := builtin.GetType("Pod").GetSpec()
	s := newOrFail(t, informerFactory, spec)
	defer s.Stop()

	// Start the source.
	ch := startOrFail(t, s)

	expectFullSync(t, ch)

	pod := &corev1.Pod{
		ObjectMeta: fakeObjectMeta,
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "c1",
					Image:           "someImage",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{
						{
							Name:     "http",
							Protocol: corev1.ProtocolTCP,
							HostPort: 80,
						},
					},
				},
			},
		},
	}

	// Add the resource.
	t.Run("Add", func(t *testing.T) {
		g := NewGomegaWithT(t)
		var err error
		if pod, err = client.CoreV1().Pods(namespace).Create(pod); err != nil {
			t.Fatalf("failed creating pod: %v", err)
		}
		expected := toEvent(resource.Added, spec, pod, pod)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	// Update the resource.
	t.Run("Update", func(t *testing.T) {
		g := NewGomegaWithT(t)
		pod.Spec.Containers[0].Name = "c2"
		pod.ResourceVersion = "rv2"
		if _, err := client.CoreV1().Pods(namespace).Update(pod); err != nil {
			t.Fatalf("failed updating pod: %v", err)
		}
		expected := toEvent(resource.Updated, spec, pod, pod)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	// Update event with no changes, should yield no events.
	t.Run("UpdateNoChange", func(t *testing.T) {
		if _, err := client.CoreV1().Pods(namespace).Update(pod); err != nil {
			t.Fatalf("failed updating pod: %v", err)
		}
		events.ExpectNone(t, ch)
	})

	// Delete the resource.
	t.Run("Delete", func(t *testing.T) {
		g := NewGomegaWithT(t)
		if err := client.CoreV1().Pods(namespace).Delete(pod.Name, nil); err != nil {
			t.Fatalf("failed deleting pod: %v", err)
		}
		expected := toEvent(resource.Deleted, spec, pod, pod)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})
}

func TestServices(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	client, informerFactory := kubeResources()

	spec := builtin.GetType("Service").GetSpec()
	s := newOrFail(t, informerFactory, spec)
	defer s.Stop()

	// Start the source.
	ch := startOrFail(t, s)

	expectFullSync(t, ch)

	svc := &corev1.Service{
		ObjectMeta: fakeObjectMeta,
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: corev1.ProtocolTCP,
					Port:     80,
				},
			},
		},
	}

	// Add the resource.
	t.Run("Add", func(t *testing.T) {
		g := NewGomegaWithT(t)
		var err error
		if svc, err = client.CoreV1().Services(namespace).Create(svc); err != nil {
			t.Fatalf("failed creating service: %v", err)
		}
		expected := toEvent(resource.Added, spec, svc, &svc.Spec)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	// Update the resource.
	t.Run("Update", func(t *testing.T) {
		g := NewGomegaWithT(t)
		svc.Spec.Ports[0].Port = 8080
		svc.ResourceVersion = "rv2"
		if _, err := client.CoreV1().Services(namespace).Update(svc); err != nil {
			t.Fatalf("failed updating service: %v", err)
		}
		expected := toEvent(resource.Updated, spec, svc, &svc.Spec)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	// Update event with no changes, should yield no events.
	t.Run("UpdateNoChange", func(t *testing.T) {
		if _, err := client.CoreV1().Services(namespace).Update(svc); err != nil {
			t.Fatalf("failed updating service: %v", err)
		}
		events.ExpectNone(t, ch)
	})

	// Delete the resource.
	t.Run("Delete", func(t *testing.T) {
		g := NewGomegaWithT(t)
		if err := client.CoreV1().Services(namespace).Delete(svc.Name, nil); err != nil {
			t.Fatalf("failed deleting service: %v", err)
		}
		expected := toEvent(resource.Deleted, spec, svc, &svc.Spec)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})
}

func TestEndpoints(t *testing.T) {
	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	client, informerFactory := kubeResources()

	spec := builtin.GetType("Endpoints").GetSpec()
	s := newOrFail(t, informerFactory, spec)
	defer s.Stop()

	// Start the source.
	ch := startOrFail(t, s)

	expectFullSync(t, ch)

	eps := &corev1.Endpoints{
		ObjectMeta: fakeObjectMeta,
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						Hostname: "fake.host.com",
						IP:       "10.40.0.0",
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Name:     "http",
						Protocol: corev1.ProtocolTCP,
						Port:     80,
					},
				},
			},
		},
	}

	// Add the resource.
	t.Run("Add", func(t *testing.T) {
		g := NewGomegaWithT(t)
		var err error
		if eps, err = client.CoreV1().Endpoints(namespace).Create(eps); err != nil {
			t.Fatalf("failed creating endpoints: %v", err)
		}
		expected := toEvent(resource.Added, spec, eps, eps)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	// Update the resource.
	t.Run("Update", func(t *testing.T) {
		g := NewGomegaWithT(t)
		eps.Subsets[0].Ports[0].Port = 8080
		eps.ResourceVersion = "rv2"
		if _, err := client.CoreV1().Endpoints(namespace).Update(eps); err != nil {
			t.Fatalf("failed updating endpoints: %v", err)
		}
		expected := toEvent(resource.Updated, spec, eps, eps)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	// Update event with no changes, should yield no events.
	t.Run("UpdateNoChange", func(t *testing.T) {
		// Changing only the resource version, should have not result in an update.
		eps.ResourceVersion = "rv3"
		if _, err := client.CoreV1().Endpoints(namespace).Update(eps); err != nil {
			t.Fatalf("failed updating endpoints: %v", err)
		}
		events.ExpectNone(t, ch)
	})

	// Delete the resource.
	t.Run("Delete", func(t *testing.T) {
		g := NewGomegaWithT(t)
		if err := client.CoreV1().Endpoints(namespace).Delete(eps.Name, nil); err != nil {
			t.Fatalf("failed deleting endpoints: %v", err)
		}
		expected := toEvent(resource.Deleted, spec, eps, eps)
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})
}

func newOrFail(t *testing.T, informerFactory informers.SharedInformerFactory, spec *schema.ResourceSpec) runtime.Source {
	t.Helper()
	s, err := builtin.New(informerFactory, *spec)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("Expected non nil source")
	}
	return s
}

func startOrFail(t *testing.T, s runtime.Source) chan resource.Event {
	t.Helper()
	g := NewGomegaWithT(t)

	ch := make(chan resource.Event, 1024)
	err := s.Start(events.ChannelHandler(ch))
	g.Expect(err).To(BeNil())

	return ch
}

func expectFullSync(t *testing.T, ch chan resource.Event) {
	g := NewGomegaWithT(t)
	// Wait for the full sync event.
	actual := events.ExpectOne(t, ch)
	g.Expect(actual).To(Equal(resource.FullSyncEvent))
}

func kubeResources() (kubernetes.Interface, informers.SharedInformerFactory) {
	client := fake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client, 0,
		informers.WithNamespace(namespace))
	return client, informerFactory
}

func toEvent(kind resource.EventKind, spec *schema.ResourceSpec, objectMeta metav1.Object,
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
				CreateTime:  fakeCreateTime,
				Labels:      objectMeta.GetLabels(),
				Annotations: objectMeta.GetAnnotations(),
			},
			Item: item,
		},
	}

	return event
}

func setDebugLogLevel() log.Level {
	prev := kubeLog.Scope.GetOutputLevel()
	kubeLog.Scope.SetOutputLevel(log.DebugLevel)
	return prev
}

func restoreLogLevel(level log.Level) {
	kubeLog.Scope.SetOutputLevel(level)
}
