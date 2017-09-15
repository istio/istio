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
	"os"
	"os/user"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/pilot/model"
	"istio.io/pilot/test/util"
)

func makeClient(t *testing.T) kubernetes.Interface {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err.Error())
	}

	kubeconfig := usr.HomeDir + "/.kube/config"

	// For Bazel sandbox we search a different location:
	if _, err = os.Stat(kubeconfig); err != nil {
		kubeconfig, _ = os.Getwd()
		kubeconfig = kubeconfig + "/config"
	}

	_, cl, err := CreateInterface(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}

	return cl
}

func eventually(f func() bool, t *testing.T) {
	interval := 64 * time.Millisecond
	for i := 0; i < 10; i++ {
		if f() {
			return
		}
		glog.Infof("Sleeping %v", interval)
		time.Sleep(interval)
		interval = 2 * interval
	}
	t.Errorf("Failed to satisfy function")
}

const (
	testService = "test"
	resync      = 1 * time.Second
)

func TestServices(t *testing.T) {
	cl := makeClient(t)
	t.Parallel()
	ns, err := util.CreateNamespace(cl)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer util.DeleteNamespace(cl, ns)

	stop := make(chan struct{})
	defer close(stop)

	ctl := NewController(cl, ControllerOptions{
		WatchedNamespace: ns,
		ResyncPeriod:     resync,
		DomainSuffix:     domainSuffix,
	})
	go ctl.Run(stop)

	hostname := serviceHostname(testService, ns, domainSuffix)

	var sds model.ServiceDiscovery = ctl
	makeService(testService, ns, cl, t)
	eventually(func() bool {
		out := sds.Services()
		glog.Info("Services: %#v", out)

		for _, item := range out {
			if item.Hostname == hostname &&
				len(item.Ports) == 1 &&
				item.Ports[0].Protocol == model.ProtocolHTTP {
				return true
			}
		}
		return false
	}, t)

	svc, exists := sds.GetService(hostname)
	if !exists {
		t.Errorf("GetService(%q) => %t, want true", hostname, exists)
	}
	if svc.Hostname != hostname {
		t.Errorf("GetService(%q) => %q", hostname, svc.Hostname)
	}

	missing := serviceHostname("does-not-exist", ns, domainSuffix)
	_, exists = sds.GetService(missing)
	if exists {
		t.Errorf("GetService(%q) => %t, want false", missing, exists)
	}
}

func makeService(n, ns string, cl kubernetes.Interface, t *testing.T) {
	_, err := cl.Core().Services(ns).Create(&v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{Name: n},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Port: 80,
					Name: "http-example",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf(err.Error())
	}
	glog.Infof("Created service %s", n)
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
			controller := makeFakeKubeAPIController()
			addPods(t, controller, c.pods...)
			for i, pod := range c.pods {
				ip := fmt.Sprintf("128.0.0.%v", i+1)
				id := fmt.Sprintf("%v/%v", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name)
				controller.pods.keys[ip] = id
			}
			addNodes(t, controller, c.nodes...)

			// Verify expected existing pod AZs
			for pod, wantAZ := range c.wantAZ {
				az, found := controller.GetPodAZ(pod)
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

func TestController_GetIstioServiceAccounts(t *testing.T) {

	controller := makeFakeKubeAPIController()

	sa1 := "acct1"
	sa2 := "acct2"
	sa3 := "acct3"
	k8sSaOnVM := "acct4"
	canonicalSaOnVM := "acctvm@gserviceaccount.com"

	pods := []*v1.Pod{
		generatePod("pod1", "nsA", sa1, "node1", map[string]string{"app": "test-app"}),
		generatePod("pod2", "nsA", sa2, "node2", map[string]string{"app": "prod-app"}),
		generatePod("pod3", "nsB", sa3, "node1", map[string]string{"app": "prod-app"}),
	}
	addPods(t, controller, pods...)

	nodes := []*v1.Node{
		generateNode("node1", map[string]string{NodeZoneLabel: "az1"}),
		generateNode("node2", map[string]string{NodeZoneLabel: "az2"}),
	}
	addNodes(t, controller, nodes...)

	// Populate pod cache.
	controller.pods.keys["128.0.0.1"] = "nsA/pod1"
	controller.pods.keys["128.0.0.2"] = "nsA/pod2"
	controller.pods.keys["128.0.0.3"] = "nsB/pod3"

	createService(controller, "svc1", "nsA",
		map[string]string{
			KubeServiceAccountsOnVMAnnotation:      k8sSaOnVM,
			CanonicalServiceAccountsOnVMAnnotation: canonicalSaOnVM},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	createService(controller, "svc2", "nsA", nil, []int32{8081}, map[string]string{"app": "staging-app"}, t)

	// Endpoints are generated by Kubernetes from pod labels and service selectors.
	// Here we manually create them for mocking purpose.
	svc1Ips := []string{"128.0.0.2"}
	svc2Ips := []string{}
	portNames := []string{"test-port"}
	createEndpoints(controller, "svc1", "nsA", portNames, svc1Ips, t)
	createEndpoints(controller, "svc2", "nsA", portNames, svc2Ips, t)

	hostname := serviceHostname("svc1", "nsA", domainSuffix)
	sa := controller.GetIstioServiceAccounts(hostname, []string{"test-port"})
	sort.Sort(sort.StringSlice(sa))
	expected := []string{
		"spiffe://" + canonicalSaOnVM,
		"spiffe://company.com/ns/nsA/sa/" + sa2,
		"spiffe://company.com/ns/nsA/sa/" + k8sSaOnVM,
	}
	if !reflect.DeepEqual(sa, expected) {
		t.Errorf("Unexpected service accounts %v (expecting %v)", sa, expected)
	}

	hostname = serviceHostname("svc2", "nsA", domainSuffix)
	sa = controller.GetIstioServiceAccounts(hostname, []string{})
	if len(sa) != 0 {
		t.Error("Failure: Expected to resolve 0 service accounts, but got: ", sa)
	}
}

func makeFakeKubeAPIController() *Controller {
	clientSet := fake.NewSimpleClientset()
	return NewController(clientSet, ControllerOptions{
		WatchedNamespace: "default",
		ResyncPeriod:     resync,
		DomainSuffix:     domainSuffix,
	})
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

func createService(controller *Controller, name, namespace string, annotations map[string]string,
	ports []int32, selector map[string]string, t *testing.T) {

	svcPorts := []v1.ServicePort{}
	for _, p := range ports {
		svcPorts = append(svcPorts, v1.ServicePort{
			Name:     "test-port",
			Port:     p,
			Protocol: "http",
		})
	}
	service := &v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			ClusterIP: "10.0.0.1", // FIXME: generate?
			Ports:     svcPorts,
			Selector:  selector,
			Type:      v1.ServiceTypeClusterIP,
		},
	}
	if err := controller.services.informer.GetStore().Add(service); err != nil {
		t.Errorf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
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
