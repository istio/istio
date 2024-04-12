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
	"testing"

	corev1 "k8s.io/api/core/v1"
	knetworking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/mesh"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	istiolog "istio.io/istio/pkg/log"
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

func fakeMeshHolder(ingressService string) mesh.Watcher {
	config := mesh.DefaultMeshConfig()
	config.IngressService = ingressService
	return mesh.NewFixedWatcher(config)
}

func makeStatusSyncer(t *testing.T, name string) *StatusSyncer {
	client := kubelib.NewFakeClient(testObjects...)
	sync := NewStatusSyncer(fakeMeshHolder(name), client)
	client.RunAndWait(test.NewStop(t))
	go sync.Run(test.NewStop(t))
	return sync
}

// nolint: unparam
func getIPs(ing clienttest.TestClient[*knetworking.Ingress], name string, ns string) func() []string {
	return func() []string {
		i := ing.Get(name, ns)
		if i == nil {
			return nil
		}
		res := []string{}
		for _, v := range i.Status.LoadBalancer.Ingress {
			if v.IP != "" {
				res = append(res, v.IP)
			} else {
				res = append(res, v.Hostname)
			}
		}
		return res
	}
}

func TestStatusController(t *testing.T) {
	statusLog.SetOutputLevel(istiolog.DebugLevel)
	c := makeStatusSyncer(t, "istio-ingress")
	ing := clienttest.Wrap(t, c.ingresses)
	svc := clienttest.Wrap(t, c.services)
	ingc := clienttest.Wrap(t, c.ingressClasses)
	ing.Create(&knetworking.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "ingress",
			Namespace:   "default",
			Annotations: map[string]string{annotation.IoKubernetesIngressClass.Name: "istio"},
		},
	})

	// Test initial state
	assert.EventuallyEqual(t, getIPs(ing, "ingress", "default"), []string{serviceIP})

	// Update service IP
	updated := ingressService.DeepCopy()
	updated.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "5.6.7.8"}}
	svc.Update(updated)
	assert.EventuallyEqual(t, getIPs(ing, "ingress", "default"), []string{"5.6.7.8"})

	// Remove service
	svc.Delete(ingressService.Name, ingressService.Namespace)
	assert.EventuallyEqual(t, getIPs(ing, "ingress", "default"), []string{})

	// Add it back
	svc.Create(updated)
	assert.EventuallyEqual(t, getIPs(ing, "ingress", "default"), []string{"5.6.7.8"})

	// Remove ingress class
	ing.Update(&knetworking.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingress",
			Namespace: "default",
		},
	})
	assert.EventuallyEqual(t, getIPs(ing, "ingress", "default"), []string{})

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
	assert.EventuallyEqual(t, getIPs(ing, "ingress", "default"), []string{})

	// Create IngressClass
	ingc.Create(&knetworking.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: ingressClassName,
		},
		Spec: knetworking.IngressClassSpec{
			Controller: IstioIngressController,
		},
	})
	assert.EventuallyEqual(t, getIPs(ing, "ingress", "default"), []string{"5.6.7.8"})
}

func TestRunningAddresses(t *testing.T) {
	t.Run("service", testRunningAddressesWithService)
	t.Run("hostname", testRunningAddressesWithHostname)
}

func testRunningAddressesWithService(t *testing.T) {
	syncer := makeStatusSyncer(t, "istio-ingress")
	address := syncer.runningAddresses()

	if len(address) != 1 || address[0] != serviceIP {
		t.Errorf("Address is not correctly set to service ip")
	}
}

func testRunningAddressesWithHostname(t *testing.T) {
	syncer := makeStatusSyncer(t, "istio-ingress-hostname")

	address := syncer.runningAddresses()

	if len(address) != 1 || address[0] != hostname {
		t.Errorf("Address is not correctly set to hostname")
	}
}

func TestRunningAddressesWithPod(t *testing.T) {
	syncer := makeStatusSyncer(t, "")

	address := syncer.runningAddresses()

	if len(address) != 1 || address[0] != nodeIP {
		t.Errorf("Address is not correctly set to node ip %v %v", address, nodeIP)
	}
}
