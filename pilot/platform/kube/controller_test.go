// Copyright 2017 Istio Authors
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

package kube

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform/test"
	"istio.io/istio/pkg/log"
)

const (
	testService = "test"
	resync      = 1 * time.Second
)

func buildExpectedControllerView(ns string) *model.ControllerView {
	modelSvcs := []*model.Service{
		{
			Hostname: testService + "." + ns + ".svc." + domainSuffix,
			Ports: model.PortList{
				&model.Port{
					Name:                 "http-example",
					Port:                 80,
					Protocol:             model.ProtocolTCP,
					AuthenticationPolicy: meshconfig.AuthenticationPolicy_INHERIT}},
			LoadBalancingDisabled: true,
		},
	}
	modelInsts := []*model.ServiceInstance{}
	return test.BuildExpectedControllerView(modelSvcs, modelInsts)
}

// The only thing being tested are the public interfaces of the controller
// namely: Handle() and Run(). Everything else is implementation detail.
func TestController(t *testing.T) {
	ns := "default"
	mockHandler := test.NewMockControllerViewHandler()
	cl := fake.NewSimpleClientset()
	controller := NewController(cl, *mockHandler.GetTicker(), ControllerOptions{
		WatchedNamespace:   ns,
		ResyncPeriod:       resync,
		DomainSuffix:       domainSuffix,
		IgnoreInformerSync: true,
	})
	addService(controller, testService, ns, cl, t)
	mockHandler.AssertControllerOK(t, controller, buildExpectedControllerView(ns))
}

func TestController_getPodAZ(t *testing.T) {

	pod1 := generatePod("pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"})
	pod2 := generatePod("pod2", "nsB", "", "node2", map[string]string{"app": "prod-app"})
	testCases := []struct {
		name   string
		pods   []*v1.Pod
		nodes  []*v1.Node
		wantAZ map[*v1.Pod]string
	}{
		{
			name: "should return correct az for given address",
			pods: []*v1.Pod{pod1, pod2},
			nodes: []*v1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2", NodeRegionLabel: "region2"}),
			},
			wantAZ: map[*v1.Pod]string{
				pod1: "region1/zone1",
				pod2: "region2/zone2",
			},
		},
		{
			name: "should return false if pod isnt in the cache",
			wantAZ: map[*v1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
		{
			name: "should return false if node isnt in the cache",
			pods: []*v1.Pod{pod1, pod2},
			wantAZ: map[*v1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
		{
			name: "should return false and empty string if node doesnt have zone label",
			pods: []*v1.Pod{pod1, pod2},
			nodes: []*v1.Node{
				generateNode("node1", map[string]string{NodeRegionLabel: "region1"}),
				generateNode("node2", map[string]string{NodeRegionLabel: "region2"}),
			},
			wantAZ: map[*v1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
		{
			name: "should return false and empty string if node doesnt have region label",
			pods: []*v1.Pod{pod1, pod2},
			nodes: []*v1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2"}),
			},
			wantAZ: map[*v1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {

			// Setup kube caches
			clientSet := fake.NewSimpleClientset()
			ticker := time.NewTicker(time.Nanosecond)
			defer ticker.Stop()
			controller := NewController(clientSet, *ticker, ControllerOptions{
				WatchedNamespace:   "default",
				ResyncPeriod:       resync,
				DomainSuffix:       domainSuffix,
				IgnoreInformerSync: true,
			})
			addPods(t, controller, c.pods...)
			for i, pod := range c.pods {
				ip := fmt.Sprintf("128.0.0.%v", i+1)
				id := fmt.Sprintf("%v/%v", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name)
				controller.pods.keys[ip] = id
			}
			addNodes(t, controller, c.nodes...)

			// Verify expected existing pod AZs
			for pod, wantAZ := range c.wantAZ {
				az, found := controller.getPodAZ(pod)
				if wantAZ != "" {
					if !reflect.DeepEqual(az, wantAZ) {
						t.Errorf("Wanted az: %s, got: %s", wantAZ, az)
					}
				} else {
					if found {
						t.Errorf("Unexpectedly found az: %s for pod: %s", az, pod.ObjectMeta.Name)
					}
				}
			}
		})
	}

}

func createEndpoints(controller *Controller, name, namespace string, portNames, ips []string, t *testing.T) {
	eas := []v1.EndpointAddress{}
	for _, ip := range ips {
		eas = append(eas, v1.EndpointAddress{IP: ip})
	}

	eps := []v1.EndpointPort{}
	for _, name := range portNames {
		eps = append(eps, v1.EndpointPort{Name: name})
	}

	endpoint := &v1.Endpoints{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []v1.EndpointSubset{{
			Addresses: eas,
			Ports:     eps,
		}},
	}
	if err := controller.endpoints.informer.GetStore().Add(endpoint); err != nil {
		t.Errorf("failed to create endpoints %s in namespace %s (error %v)", name, namespace, err)
	}
}

func addService(controller *Controller, n, ns string, cl kubernetes.Interface, t *testing.T) {
	if err := controller.services.informer.GetStore().Add(&v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{Name: n, Namespace: ns},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Port: 80,
					Name: "http-example",
				},
			},
		},
	}); err != nil {
		t.Fatalf(err.Error())
	}
	if log.DebugEnabled() {
		log.Debugf("Created service %s", n)
	}
}

func addPods(t *testing.T, controller *Controller, pods ...*v1.Pod) {
	for _, pod := range pods {
		if err := controller.pods.informer.GetStore().Add(pod); err != nil {
			t.Errorf("Cannot create pod in namespace %s (error: %v)", pod.ObjectMeta.Namespace, err)
		}
	}
}

func generatePod(name, namespace, saName, node string, labels map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      name,
			Labels:    labels,
			Namespace: namespace,
		},
		Spec: v1.PodSpec{
			ServiceAccountName: saName,
			NodeName:           node,
		},
	}
}

func generateNode(name string, labels map[string]string) *v1.Node {
	return &v1.Node{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func addNodes(t *testing.T, controller *Controller, nodes ...*v1.Node) {
	for _, node := range nodes {
		if err := controller.nodes.informer.GetStore().Add(node); err != nil {
			t.Errorf("Cannot create node %s (error: %v)", node.Name, err)
		}
	}
}
