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
package apiserver_test

import (
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/testing/k8smeta"
	"istio.io/istio/galley/pkg/testing/mock"
	"istio.io/pkg/log"
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

func TestBasic(t *testing.T) {
	g := NewGomegaWithT(t)

	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	k := mock.NewKube()
	client, err := k.KubeClient()
	g.Expect(err).To(BeNil())

	// Start the source.
	s := newOrFail(t, k, k8smeta.MustGet().KubeSource().Resources(), nil)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.EventsWithoutOrigins).Should(HaveLen(7))
	for i := 0; i < 7; i++ {
		g.Expect(acc.EventsWithoutOrigins()[i].Kind).Should(Equal(event.FullSync))
	}

	acc.Clear()

	node := &corev1.Node{
		ObjectMeta: fakeObjectMeta,
		Spec: corev1.NodeSpec{
			PodCIDR: "10.40.0.0/24",
		},
	}
	node.Namespace = "" // nodes don't have namespaces.

	// Add the resource.
	if node, err = client.CoreV1().Nodes().Create(node); err != nil {
		t.Fatalf("failed creating node: %v", err)
	}

	expected := event.AddFor(k8smeta.K8SCoreV1Nodes, toResource(node, &node.Spec))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))
}

func TestNodes(t *testing.T) {
	g := NewGomegaWithT(t)

	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	k := mock.NewKube()
	client, err := k.KubeClient()
	g.Expect(err).To(BeNil())

	// Start the source.
	s := newOrFail(t, k, k8smeta.MustGet().KubeSource().Resources(), nil)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.EventsWithoutOrigins).Should(HaveLen(7))
	for i := 0; i < 7; i++ {
		g.Expect(acc.EventsWithoutOrigins()[i].Kind).Should(Equal(event.FullSync))
	}
	acc.Clear()

	node := &corev1.Node{
		ObjectMeta: fakeObjectMeta,
		Spec: corev1.NodeSpec{
			PodCIDR: "10.40.0.0/24",
		},
	}
	node.Namespace = "" // nodes don't have namespaces.

	// Add the resource.
	if node, err = client.CoreV1().Nodes().Create(node); err != nil {
		t.Fatalf("failed creating node: %v", err)
	}

	expected := event.AddFor(k8smeta.K8SCoreV1Nodes, toResource(node, &node.Spec))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))

	acc.Clear()

	// Update the resource.
	node = node.DeepCopy()
	node.Spec.PodCIDR = "10.20.0.0/32"
	node.ResourceVersion = "rv2"
	if _, err = client.CoreV1().Nodes().Update(node); err != nil {
		t.Fatalf("failed updating node: %v", err)
	}

	expected = event.UpdateFor(k8smeta.K8SCoreV1Nodes, toResource(node, &node.Spec))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))

	acc.Clear()

	if _, err = client.CoreV1().Nodes().Update(node); err != nil {
		t.Fatalf("failed updating node: %v", err)
	}
	g.Consistently(acc.EventsWithoutOrigins).Should(BeEmpty())

	acc.Clear()

	// Delete the resource.
	if err = client.CoreV1().Nodes().Delete(node.Name, nil); err != nil {
		t.Fatalf("failed deleting node: %v", err)
	}
	expected = event.DeleteForResource(k8smeta.K8SCoreV1Nodes, toResource(node, &node.Spec))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))
}

func TestPods(t *testing.T) {
	g := NewGomegaWithT(t)

	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	k := mock.NewKube()
	client, err := k.KubeClient()
	g.Expect(err).To(BeNil())

	// Start the source.
	s := newOrFail(t, k, k8smeta.MustGet().KubeSource().Resources(), nil)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.EventsWithoutOrigins).Should(HaveLen(7))
	for i := 0; i < 7; i++ {
		g.Expect(acc.EventsWithoutOrigins()[i].Kind).Should(Equal(event.FullSync))
	}
	acc.Clear()

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

	if pod, err = client.CoreV1().Pods(namespace).Create(pod); err != nil {
		t.Fatalf("failed creating pod: %v", err)
	}
	expected := event.AddFor(k8smeta.K8SCoreV1Pods, toResource(pod, pod))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))

	acc.Clear()

	// Update the resource.
	pod = pod.DeepCopy()
	pod.Spec.Containers[0].Name = "c2"
	pod.ResourceVersion = "rv2"
	if _, err = client.CoreV1().Pods(namespace).Update(pod); err != nil {
		t.Fatalf("failed updating pod: %v", err)
	}
	expected = event.UpdateFor(k8smeta.K8SCoreV1Pods, toResource(pod, pod))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))

	acc.Clear()

	// Update event with no changes, should yield no events.
	if _, err = client.CoreV1().Pods(namespace).Update(pod); err != nil {
		t.Fatalf("failed updating pod: %v", err)
	}
	g.Consistently(acc.EventsWithoutOrigins).Should(BeEmpty())

	acc.Clear()

	// Delete the resource.
	if err = client.CoreV1().Pods(namespace).Delete(pod.Name, nil); err != nil {
		t.Fatalf("failed deleting pod: %v", err)
	}
	expected = event.DeleteForResource(k8smeta.K8SCoreV1Pods, toResource(pod, pod))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))
}

func TestServices(t *testing.T) {
	g := NewGomegaWithT(t)

	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	k := mock.NewKube()
	client, err := k.KubeClient()
	g.Expect(err).To(BeNil())

	// Start the source.
	s := newOrFail(t, k, k8smeta.MustGet().KubeSource().Resources(), nil)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.EventsWithoutOrigins).Should(HaveLen(7))
	for i := 0; i < 7; i++ {
		g.Expect(acc.EventsWithoutOrigins()[i].Kind).Should(Equal(event.FullSync))
	}
	acc.Clear()

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
	if svc, err = client.CoreV1().Services(namespace).Create(svc); err != nil {
		t.Fatalf("failed creating service: %v", err)
	}
	expected := event.AddFor(k8smeta.K8SCoreV1Services, toResource(svc, &svc.Spec))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))

	acc.Clear()

	// Update the resource.
	svc = svc.DeepCopy()
	svc.Spec.Ports[0].Port = 8080
	svc.ResourceVersion = "rv2"
	if _, err = client.CoreV1().Services(namespace).Update(svc); err != nil {
		t.Fatalf("failed updating service: %v", err)
	}
	expected = event.UpdateFor(k8smeta.K8SCoreV1Services, toResource(svc, &svc.Spec))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))

	acc.Clear()

	// Update event with no changes, should yield no events.
	if _, err = client.CoreV1().Services(namespace).Update(svc); err != nil {
		t.Fatalf("failed updating service: %v", err)
	}
	g.Consistently(acc.EventsWithoutOrigins).Should(BeEmpty())

	acc.Clear()

	// Delete the resource.
	if err = client.CoreV1().Services(namespace).Delete(svc.Name, nil); err != nil {
		t.Fatalf("failed deleting service: %v", err)
	}
	expected = event.DeleteForResource(k8smeta.K8SCoreV1Services, toResource(svc, &svc.Spec))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))
}

func TestEndpoints(t *testing.T) {
	g := NewGomegaWithT(t)

	// Set the log level to debug for codecov.
	prevLevel := setDebugLogLevel()
	defer restoreLogLevel(prevLevel)

	k := mock.NewKube()
	client, err := k.KubeClient()
	g.Expect(err).To(BeNil())

	// Start the source.
	s := newOrFail(t, k, k8smeta.MustGet().KubeSource().Resources(), nil)
	acc := start(s)
	defer s.Stop()

	g.Eventually(acc.EventsWithoutOrigins).Should(HaveLen(7))
	for i := 0; i < 7; i++ {
		g.Expect(acc.EventsWithoutOrigins()[i].Kind).Should(Equal(event.FullSync))
	}
	acc.Clear()

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
	if eps, err = client.CoreV1().Endpoints(namespace).Create(eps); err != nil {
		t.Fatalf("failed creating endpoints: %v", err)
	}
	expected := event.AddFor(k8smeta.K8SCoreV1Endpoints, toResource(eps, eps))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))

	acc.Clear()

	// Update the resource.
	eps = eps.DeepCopy()
	eps.Subsets[0].Ports[0].Port = 8080
	eps.ResourceVersion = "rv2"
	if _, err = client.CoreV1().Endpoints(namespace).Update(eps); err != nil {
		t.Fatalf("failed updating endpoints: %v", err)
	}
	expected = event.UpdateFor(k8smeta.K8SCoreV1Endpoints, toResource(eps, eps))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))

	acc.Clear()

	// Update event with no changes, should yield no events.
	// Changing only the resource version, should have not result in an update.
	eps = eps.DeepCopy()
	eps.ResourceVersion = "rv3"
	if _, err = client.CoreV1().Endpoints(namespace).Update(eps); err != nil {
		t.Fatalf("failed updating endpoints: %v", err)
	}
	g.Consistently(acc.EventsWithoutOrigins).Should(BeEmpty())

	// Delete the resource.
	if err = client.CoreV1().Endpoints(namespace).Delete(eps.Name, nil); err != nil {
		t.Fatalf("failed deleting endpoints: %v", err)
	}
	expected = event.DeleteForResource(k8smeta.K8SCoreV1Endpoints, toResource(eps, eps))
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(expected))
}

func toResource(objectMeta metav1.Object, item proto.Message) *resource.Entry {
	return &resource.Entry{
		Metadata: resource.Metadata{
			Name:        resource.NewName(objectMeta.GetNamespace(), objectMeta.GetName()),
			Version:     resource.Version(objectMeta.GetResourceVersion()),
			CreateTime:  fakeCreateTime,
			Labels:      objectMeta.GetLabels(),
			Annotations: objectMeta.GetAnnotations(),
		},
		Item: item,
	}
}

func setDebugLogLevel() log.Level {
	prev := scope.Source.GetOutputLevel()
	scope.Source.SetOutputLevel(log.DebugLevel)
	return prev
}

func restoreLogLevel(level log.Level) {
	scope.Source.SetOutputLevel(level)
}
