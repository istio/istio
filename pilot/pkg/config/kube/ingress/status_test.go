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

package ingress

import (
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	knetworking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/api/annotation"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

var (
	serviceIP = "1.2.3.4"
	hostname  = "foo.bar.com"
	nodeIP    = "10.0.0.2"
)

var ingressService = &corev1.Service{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "istio-ingress",
		Namespace: IngressNamespace,
	},
	Status: corev1.ServiceStatus{
		LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{
				IP: serviceIP,
			}},
		},
	},
}

var testObjects = []runtime.Object{
	&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingressgateway",
			Namespace: "istio-system",
			Labels: map[string]string{
				"istio": "ingressgateway",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "foo_node",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	},
	ingressService,
	&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ingress-hostname",
			Namespace: IngressNamespace,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{
					Hostname: hostname,
				}},
			},
		},
	},
	&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo_node",
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeExternalIP,
					Address: nodeIP,
				},
			},
		},
	},
}

type TestStatusQueue struct {
	mu    sync.Mutex
	state map[status.Resource]any
}

func (t *TestStatusQueue) EnqueueStatusUpdateResource(context any, target status.Resource) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state[target] = context
}

func (t *TestStatusQueue) Statuses() []any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return maps.Values(t.state)
}

var _ status.Queue = &TestStatusQueue{}

func fakeMeshHolder(ingressService string) meshwatcher.WatcherCollection {
	config := mesh.DefaultMeshConfig()
	config.IngressService = ingressService
	return meshwatcher.NewTestWatcher(config)
}

func makeControllerWithStatus(t *testing.T, name string) (*Controller, kube.Client) {
	client := kube.NewFakeClient(testObjects...)

	c := NewController(
		client,
		fakeMeshHolder(name),
		kubecontroller.Options{KrtDebugger: krt.GlobalDebugHandler},
		nil,
	)

	stop := test.NewStop(t)
	go c.Run(stop)
	client.RunAndWait(stop)

	return c, client
}

func TestStatusController(t *testing.T) {
	controller, client := makeControllerWithStatus(t, "istio-ingress")

	ing := clienttest.NewWriter[*knetworking.Ingress](t, client)
	svc := clienttest.NewWriter[*corev1.Service](t, client)
	ingc := clienttest.NewWriter[*knetworking.IngressClass](t, client)
	sq := &TestStatusQueue{
		state: map[status.Resource]any{},
	}
	controller.status.SetQueue(sq)

	ing.Create(&knetworking.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "ingress",
			Namespace:   "default",
			Annotations: map[string]string{annotation.IoKubernetesIngressClass.Name: "istio"},
		},
	})

	getIPs := func() []string {
		statuses := sq.Statuses()
		if len(statuses) == 1 {
			ing := statuses[0].(*knetworking.IngressStatus).LoadBalancer.Ingress
			res := []string{}
			for _, v := range ing {
				if v.IP != "" {
					res = append(res, v.IP)
				} else {
					res = append(res, v.Hostname)
				}
			}
			return res
		}

		return nil
	}

	assert.EventuallyEqual(t, getIPs, []string{serviceIP})

	// Update service IP
	updated := ingressService.DeepCopy()
	updated.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "5.6.7.8"}}
	svc.Update(updated)
	assert.EventuallyEqual(t, getIPs, []string{"5.6.7.8"})

	// Remove service
	svc.Delete(ingressService.Name, ingressService.Namespace)
	assert.EventuallyEqual(t, getIPs, []string{})

	// Add it back
	svc.Create(updated)
	assert.EventuallyEqual(t, getIPs, []string{"5.6.7.8"})

	// Remove ingress class
	ing.Update(&knetworking.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingress",
			Namespace: "default",
		},
	})
	assert.EventuallyEqual(t, getIPs, []string{})

	ingressClassName := "istio"
	// Set IngressClassName
	ing.Update(&knetworking.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingress",
			Namespace: "default",
		},
		Spec: knetworking.IngressSpec{
			IngressClassName: &ingressClassName,
		},
	})
	assert.EventuallyEqual(t, getIPs, []string{})

	// Create IngressClass
	ingc.Create(&knetworking.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: ingressClassName,
		},
		Spec: knetworking.IngressClassSpec{
			Controller: IstioIngressController,
		},
	})
	assert.EventuallyEqual(t, getIPs, []string{"5.6.7.8"})
}

func TestRunningAddresses(t *testing.T) {
	t.Run("service", testRunningAddressesWithService)
	t.Run("hostname", testRunningAddressesWithHostname)
	t.Run("pod", testRunningAddressesWithPod)
}

type informers struct {
	mesh            meshwatcher.WatcherCollection
	services        krt.Collection[*corev1.Service]
	nodes           krt.Collection[*corev1.Node]
	pods            krt.Collection[*corev1.Pod]
	podsByNamespace krt.Index[string, *corev1.Pod]
}

func makeTestInformers(t *testing.T, name string) informers {
	stop := test.NewStop(t)
	meshHolder := fakeMeshHolder(name)
	client := kube.NewFakeClient(testObjects...)
	opts := krt.NewOptionsBuilder(stop, "ingress", krt.GlobalDebugHandler, nil)
	services := krt.WrapClient(
		kclient.NewFiltered[*corev1.Service](client, kclient.Filter{
			ObjectFilter: client.ObjectFilter(),
		}),
		opts.WithName("informer/Services")...,
	)
	nodes := krt.NewInformerFiltered[*corev1.Node](client, kclient.Filter{
		ObjectFilter:    client.ObjectFilter(),
		ObjectTransform: kube.StripNodeUnusedFields,
	}, opts.WithName("informer/Nodes")...)
	pods := krt.NewInformerFiltered[*corev1.Pod](client, kclient.Filter{
		ObjectFilter:    client.ObjectFilter(),
		ObjectTransform: kube.StripPodUnusedFields,
	}, opts.WithName("informer/Pods")...)
	inf := informers{
		mesh:            meshHolder,
		services:        services,
		nodes:           nodes,
		pods:            pods,
		podsByNamespace: krt.NewNamespaceIndex(pods),
	}
	client.RunAndWait(stop)
	kube.WaitForCacheSync("test", stop, services.HasSynced, nodes.HasSynced, pods.HasSynced)

	return inf
}

func testRunningAddressesWithService(t *testing.T) {
	informers := makeTestInformers(t, "istio-ingress")

	address := runningAddresses(
		krt.TestingDummyContext{},
		informers.mesh,
		informers.services,
		informers.nodes,
		informers.pods,
		informers.podsByNamespace,
	)

	if len(address) != 1 || address[0] != serviceIP {
		t.Errorf("Address is not correctly set to service ip")
	}
}

func testRunningAddressesWithHostname(t *testing.T) {
	informers := makeTestInformers(t, "istio-ingress-hostname")

	address := runningAddresses(
		krt.TestingDummyContext{},
		informers.mesh,
		informers.services,
		informers.nodes,
		informers.pods,
		informers.podsByNamespace,
	)

	if len(address) != 1 || address[0] != hostname {
		t.Errorf("Address is not correctly set to hostname")
	}
}

func testRunningAddressesWithPod(t *testing.T) {
	informers := makeTestInformers(t, "")

	address := runningAddresses(
		krt.TestingDummyContext{},
		informers.mesh,
		informers.services,
		informers.nodes,
		informers.pods,
		informers.podsByNamespace,
	)

	if len(address) != 1 || address[0] != nodeIP {
		t.Errorf("Address is not correctly set to node ip %v %v", address, nodeIP)
	}
}
