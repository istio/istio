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
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
)

func makeClient(t *testing.T) kubernetes.Interface {
	// Don't depend on symlink, and don't use real cluster.
	// This is the circleci config matching localhost (testEnvLocalK8S.sh start)
	cwd, _ := os.Getwd()
	kubeconfig := cwd + "/../../../../.circleci/config"
	cl, err := CreateInterface(kubeconfig)
	if err != nil {
		t.Skip("No local k8s env, skipping test", err, kubeconfig)
	}

	return cl
}

const (
	testService = "test"
	resync      = 1 * time.Second
)

type FakeUpdater struct {
}

func NewFakeUpdater() *FakeUpdater {
	return &FakeUpdater{}
}

func (*FakeUpdater) ConfigUpdate(bool) {

}

// FakeXdsUpdater is used to test the registry.
type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan XdsEvent
}

// XdsEvent is used to watch XdsEvents
type XdsEvent struct {
	// Type of the event
	Type string

	// The id of the event
	ID string
}

// NewFakeXDS creates a XdsUpdater reporting events via a channel.
func NewFakeXDS() *FakeXdsUpdater {
	return &FakeXdsUpdater{
		Events: make(chan XdsEvent, 100),
	}
}

func (fx *FakeXdsUpdater) EDSUpdate(shard, hostname string, entry []*model.IstioEndpoint) error {
	select {
	case fx.Events <- XdsEvent{Type: "eds", ID: hostname}:
	default:
	}
	return nil

}

// SvcUpdate is called when a service port mapping definition is updated.
// This interface is WIP - labels, annotations and other changes to service may be
// updated to force a EDS and CDS recomputation and incremental push, as it doesn't affect
// LDS/RDS.
func (fx *FakeXdsUpdater) SvcUpdate(shard, hostname string, ports map[string]uint32, rports map[uint32]string) {
	select {
	case fx.Events <- XdsEvent{Type: "service", ID: hostname}:
	default:
	}
}

func (fx *FakeXdsUpdater) WorkloadUpdate(id string, labels map[string]string, annotations map[string]string) {
	select {
	case fx.Events <- XdsEvent{Type: "workload", ID: id}:
	default:
	}
}

func (fx *FakeXdsUpdater) Wait(et string) *XdsEvent {
	t := time.NewTimer(5 * time.Second)

	for {
		select {
		case e := <-fx.Events:
			if e.Type == et {
				return &e
			}
			continue
		case <-t.C:
			return nil
		}
	}
}

// Clear any pending event
func (fx *FakeXdsUpdater) Clear() {
	wait := true
	for wait {
		select {
		case <-fx.Events:
		default:
			wait = false
			break
		}
	}
}

// Uses local or remote k8s cluster - requires KUBECONFIG or ~/.kube/config
// Will create a temp namespace
func TestServices(t *testing.T) {
	t.Run("localApiserver", func(t *testing.T) {
		cl, fx := newLocalController(t)
		defer cl.Stop()
		testServices(t, cl, fx)
	})
	t.Run("fakeApiserver", func(t *testing.T) {
		t.Parallel()
		cl, fx := newFakeController(t)
		defer cl.Stop()
		testServices(t, cl, fx)
	})
}

func newLocalController(t *testing.T) (*Controller, *FakeXdsUpdater) {
	fx := NewFakeXDS()
	ki := makeClient(t)
	ctl := NewController(ki, ControllerOptions{
		WatchedNamespace: "",
		ResyncPeriod:     resync,
		DomainSuffix:     domainSuffix,
		XDSUpdater:       fx,
		ConfigUpdater:    NewFakeUpdater(),
		stop:             make(chan struct{}),
	})
	go ctl.Run(ctl.stop)
	return ctl, fx
}

func newFakeController(t *testing.T) (*Controller, *FakeXdsUpdater) {
	fx := NewFakeXDS()
	clientSet := fake.NewSimpleClientset()
	c := NewController(clientSet, ControllerOptions{
		WatchedNamespace: "", // tests create resources in multiple ns
		ResyncPeriod:     resync,
		DomainSuffix:     domainSuffix,
		XDSUpdater:       fx,
		ConfigUpdater:    NewFakeUpdater(),
		stop:             make(chan struct{}),
	})
	go c.Run(c.stop)
	return c, fx
}

// The test can be run against local api server or fake apiserver.
// Should not be run against a real k8s.
func testServices(t *testing.T, ctl *Controller, fx *FakeXdsUpdater) {
	ns, err := util.CreateNamespace(ctl.client)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer util.DeleteNamespace(ctl.client, ns)

	stop := make(chan struct{})
	defer close(stop)

	hostname := serviceHostname(testService, ns, domainSuffix)

	var sds model.ServiceDiscovery = ctl
	// "test", ports: http-example on 80
	makeService(testService, ns, ctl.client, t)

	test.Eventually(t, "successfully added a service", func() bool {
		out, clientErr := sds.Services()
		if clientErr != nil {
			return false
		}
		log.Infof("Services: %#v", out)

		for _, item := range out {
			if item.Hostname == hostname &&
				len(item.Ports) == 1 &&
				item.Ports[0].Protocol == model.ProtocolHTTP {
				return true
			}
		}
		return false
	})

	// 2 ports 1001, 2 IPs
	createEndpoints(ctl, testService, ns, []string{"http-example", "foo"}, []string{"10.1.1.1", "10.1.1.2"}, t)

	test.Eventually(t, "successfully created endpoints", func() bool {
		ep, anotherErr := sds.InstancesByPort(hostname, 80, nil)
		if anotherErr != nil {
			t.Errorf("error gettings instance by port: %v", anotherErr)
			return false
		}
		if len(ep) == 2 {
			return true
		}
		return false
	})

	svc, err := sds.GetService(hostname)
	if err != nil {
		t.Errorf("GetService(%q) encountered unexpected error: %v", hostname, err)
	}
	if svc == nil {
		t.Errorf("GetService(%q) => should exists", hostname)
	}
	if svc.Hostname != hostname {
		t.Errorf("GetService(%q) => %q", hostname, svc.Hostname)
	}

	ep, err := sds.InstancesByPort(hostname, 80, nil)
	if err != nil {
		t.Errorf("GetInstancesByPort() encountered unexpected error: %v", err)
	}
	if len(ep) != 2 {
		t.Errorf("Invalid response for GetInstancesByPort %v", ep)
	}

	missing := serviceHostname("does-not-exist", ns, domainSuffix)
	svc, err = sds.GetService(missing)
	if err != nil {
		t.Errorf("GetService(%q) encountered unexpected error: %v", missing, err)
	}
	if svc != nil {
		t.Errorf("GetService(%q) => %s, should not exist", missing, svc.Hostname)
	}
}

func makeService(n, ns string, cl kubernetes.Interface, t *testing.T) {
	_, err := cl.CoreV1().Services(ns).Create(&v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{Name: n},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Port:     80,
					Name:     "http-example",
					Protocol: v1.ProtocolTCP, // Not added automatically by fake
				},
			},
		},
	})
	if err != nil {
		t.Log("Service already created (rerunning test)")
	}
	log.Infof("Created service %s", n)
}

func TestController_getPodAZ(t *testing.T) {
	t.Parallel()
	pod1 := generatePod("128.0.1.1", "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pod2 := generatePod("128.0.1.2", "pod2", "nsB", "", "node2", map[string]string{"app": "prod-app"}, map[string]string{})
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
			controller, fx := newFakeController(t)
			defer controller.Stop()
			addNodes(t, controller, c.nodes...)
			addPods(t, controller, c.pods...)
			for range c.pods {
				fx.Wait("workload")
			}

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

func TestGetProxyServiceInstances(t *testing.T) {
	t.Parallel()
	for i := 0; i < 5; i++ {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			testGetProxyServiceInstances(t)
		})
	}
}

func testGetProxyServiceInstances(t *testing.T) {
	controller, fx := newFakeController(t)
	defer controller.Stop()

	k8sSaOnVM := "acct4"
	canonicalSaOnVM := "acctvm2@gserviceaccount2.com"

	createService(controller, "svc1", "nsA",
		map[string]string{
			KubeServiceAccountsOnVMAnnotation:      k8sSaOnVM,
			CanonicalServiceAccountsOnVMAnnotation: canonicalSaOnVM},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	fx.Wait("service")

	// Endpoints are generated by Kubernetes from pod labels and service selectors.
	// Here we manually create them for mocking purpose.
	svc1Ips := []string{"128.0.0.1"}
	portNames := []string{"test-port"}
	createEndpoints(controller, "svc1", "nsA", portNames, svc1Ips, t)
	fx.Wait("eds")

	var svcNode model.Proxy
	svcNode.Type = model.Ingress
	svcNode.IPAddress = "128.0.0.1"
	svcNode.ID = "pod1.nsA"
	svcNode.Domain = "nsA.svc.cluster.local"
	services, err := controller.GetProxyServiceInstances(&svcNode)
	if err != nil {
		t.Errorf("client encountered error during GetProxyServiceInstances(): %v", err)
	}

	if len(services) != 1 {
		t.Fatalf("GetProxyServiceInstances() returned wrong # of endpoints => %v, want 1", len(services))
	}

	hostname := serviceHostname("svc1", "nsA", domainSuffix)
	if services[0].Service.Hostname != hostname {
		t.Errorf("GetProxyServiceInstances() wrong service instance returned => hostname %q, want %q",
			services[0].Service.Hostname, hostname)
	}
}

func TestController_GetIstioServiceAccounts(t *testing.T) {
	controller, fx := newFakeController(t)
	defer controller.Stop()

	sa1 := "acct1"
	sa2 := "acct2"
	sa3 := "acct3"
	k8sSaOnVM := "acct4"
	canonicalSaOnVM := "acctvm@gserviceaccount.com"

	pods := []*v1.Pod{
		generatePod("128.0.1.3", "pod1", "nsA", sa1, "node1", map[string]string{"app": "test-app"}, map[string]string{}),
		generatePod("128.0.1.4", "pod2", "nsA", sa2, "node2", map[string]string{"app": "prod-app"}, map[string]string{}),
		generatePod("128.0.1.5", "pod3", "nsB", sa3, "node1", map[string]string{"app": "prod-app"}, map[string]string{}),
	}
	addPods(t, controller, pods...)
	for i := 0; i < 3; i++ {
		<-fx.Events
	}
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
	for i := 0; i < 2; i++ {
		<-fx.Events
	}
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

func TestWorkloadHealthCheckInfo(t *testing.T) {
	controller, fx := newFakeController(t)
	defer controller.Stop()

	pods := []*v1.Pod{
		generatePodWithProbes("128.0.0.1", "pod1", "nsa1", "", "node1", "/ready", intstr.Parse("8080"), "/live", intstr.Parse("9090")),
	}
	addPods(t, controller, pods...)

	fx.Wait("workload")

	probes := controller.WorkloadHealthCheckInfo("128.0.0.1")

	expected := []*model.Probe{
		{
			Path: "/ready",
			Port: &model.Port{
				Name:     "mgmt-8080",
				Port:     8080,
				Protocol: model.ProtocolHTTP,
			},
		},
		{
			Path: "/live",
			Port: &model.Port{
				Name:     "mgmt-9090",
				Port:     9090,
				Protocol: model.ProtocolHTTP,
			},
		},
	}

	if len(probes) != len(expected) {
		t.Errorf("Expecting %d probes but got %d\r\n", len(expected), len(probes))
		return
	}

	for i, exp := range expected {
		if !reflect.DeepEqual(exp, probes[i]) {
			t.Errorf("Probe %d, got:\n%#v\nwanted:\n%#v\n", i, probes[i], exp)
		}
	}
}

func TestWorkloadHealthCheckInfoPrometheusScrape(t *testing.T) {
	controller, fx := newFakeController(t)
	defer controller.Stop()

	pods := []*v1.Pod{
		generatePod("128.0.1.6", "pod1", "nsA", "", "node1", map[string]string{"app": "test-app"},
			map[string]string{PrometheusScrape: "true"}),
	}
	addPods(t, controller, pods...)
	fx.Wait("workload")

	controller.pods.keys["128.0.1.6"] = "nsA/pod1"

	probes := controller.WorkloadHealthCheckInfo("128.0.1.6")

	expected := &model.Probe{
		Path: PrometheusPathDefault,
	}

	if len(probes) != 1 {
		t.Errorf("Expecting 1 probe but got %d\r\n", len(probes))
	} else if !reflect.DeepEqual(expected, probes[0]) {
		t.Errorf("Probe got:\n%#v\nwanted:\n%#v\n", probes[0], expected)
	}
}

func TestWorkloadHealthCheckInfoPrometheusPath(t *testing.T) {
	controller, fx := newFakeController(t)
	defer controller.Stop()

	pods := []*v1.Pod{
		generatePod("128.0.1.7", "pod1", "nsA", "", "node1", map[string]string{"app": "test-app"},
			map[string]string{PrometheusScrape: "true", PrometheusPath: "/other"}),
	}
	addPods(t, controller, pods...)
	fx.Wait("workload")

	controller.pods.keys["128.0.1.7"] = "nsA/pod1"

	probes := controller.WorkloadHealthCheckInfo("128.0.1.7")

	expected := &model.Probe{
		Path: "/other",
	}

	if len(probes) != 1 {
		t.Errorf("Expecting 1 probe but got %d\r\n", len(probes))
	} else if !reflect.DeepEqual(expected, probes[0]) {
		t.Errorf("Probe got:\n%#v\nwanted:\n%#v\n", probes[0], expected)
	}
}

func TestWorkloadHealthCheckInfoPrometheusPort(t *testing.T) {
	controller, fx := newFakeController(t)
	defer controller.Stop()

	pods := []*v1.Pod{
		generatePod("128.0.1.8", "pod1", "nsA", "", "node1", map[string]string{"app": "test-app"},
			map[string]string{PrometheusScrape: "true", PrometheusPort: "3210"}),
	}
	addPods(t, controller, pods...)
	fx.Wait("workload")
	controller.pods.keys["128.0.1.8"] = "nsA/pod1"

	probes := controller.WorkloadHealthCheckInfo("128.0.1.8")

	expected := &model.Probe{
		Port: &model.Port{
			Port: 3210,
		},
		Path: PrometheusPathDefault,
	}

	if len(probes) != 1 {
		t.Errorf("Expecting 1 probe but got %d\r\n", len(probes))
	} else if !reflect.DeepEqual(expected, probes[0]) {
		t.Errorf("Probe got:\n%#v\nwanted:\n%#v\n", probes[0], expected)
	}
}

func createEndpoints(controller *Controller, name, namespace string, portNames, ips []string, t *testing.T) {
	eas := []v1.EndpointAddress{}
	for _, ip := range ips {
		eas = append(eas, v1.EndpointAddress{IP: ip})
	}

	eps := []v1.EndpointPort{}
	for _, name := range portNames {
		eps = append(eps, v1.EndpointPort{Name: name, Port: 1001})
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
	//if err := controller.endpoints.informer.GetStore().Add(endpoint); err != nil {
	if _, err := controller.client.CoreV1().Endpoints(namespace).Create(endpoint); err != nil {
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
	if _, err := controller.client.CoreV1().Services(namespace).Create(service); err != nil {
		t.Errorf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

func addPods(t *testing.T, controller *Controller, pods ...*v1.Pod) {
	for _, pod := range pods {
		newPod, err := controller.client.CoreV1().Pods(pod.Namespace).Create(pod)
		if err != nil {
			t.Errorf("Cannot create %s in namespace %s (error: %v)", pod.ObjectMeta.Name, pod.ObjectMeta.Namespace, err)
		}
		// Apiserver doesn't allow Create/Update to modify the pod status. Creating doesn't result in
		// events - since PodIP will be "".
		newPod.Status.PodIP = pod.Status.PodIP
		newPod.Status.Phase = v1.PodRunning
		controller.client.CoreV1().Pods(pod.Namespace).UpdateStatus(newPod)
	}
}

func generatePod(ip, name, namespace, saName, node string, labels map[string]string, annotations map[string]string) *v1.Pod {
	automount := false
	return &v1.Pod{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
			Namespace:   namespace,
		},
		Spec: v1.PodSpec{
			ServiceAccountName:           saName,
			NodeName:                     node,
			AutomountServiceAccountToken: &automount,
			// Validation requires this
			Containers: []v1.Container{
				v1.Container{
					Name:  "test",
					Image: "ununtu",
				},
			},
		},
		// The cache controller uses this as key, required by our impl.
		Status: v1.PodStatus{
			PodIP:  ip,
			HostIP: ip,
			Phase:  v1.PodRunning,
		},
	}
}

func generatePodWithProbes(ip, name, namespace, saName, node string, readinessPath string, readinessPort intstr.IntOrString,
	livenessPath string, livenessPort intstr.IntOrString) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.PodSpec{
			ServiceAccountName: saName,
			NodeName:           node,
			Containers: []v1.Container{{
				ReadinessProbe: &v1.Probe{
					Handler: v1.Handler{
						HTTPGet: &v1.HTTPGetAction{
							Path: readinessPath,
							Port: readinessPort,
						},
					},
				},
				LivenessProbe: &v1.Probe{
					Handler: v1.Handler{
						HTTPGet: &v1.HTTPGetAction{
							Path: livenessPath,
							Port: livenessPort,
						},
					},
				},
			}},
		},
		// The cache controller uses this as key, required by our impl.
		Status: v1.PodStatus{
			PodIP:  ip,
			HostIP: ip,
			Phase:  v1.PodRunning,
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
		if _, err := controller.client.CoreV1().Nodes().Create(node); err != nil {
			//if err := controller.nodes.informer.GetStore().Add(node); err != nil {
			t.Errorf("Cannot create node %s (error: %v)", node.Name, err)
		}
	}
}
