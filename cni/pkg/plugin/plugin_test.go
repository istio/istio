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

package plugin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	cniv1 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/testutils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

var (
	ifname           = "eth0"
	sandboxDirectory = "/tmp"
	currentVersion   = "1.0.0"
	testPodName      = "testPodName"
	testNSName       = "testNS"
	k8Args           = fmt.Sprintf("K8S_POD_NAMESPACE=%s;K8S_POD_NAME=%s", testNSName, testPodName)
	invalidVersion   = "0.1.0"
	preVersion       = "0.2.0"
	ambientEnabled   = true

	getKubePodInfoCalled = false
	nsenterFuncCalled    = false
	cniAddServerCalled   = false

	testContainers                = sets.New("mockContainer", "foo-init")
	testLabels                    = map[string]string{}
	testAnnotations               = map[string]string{}
	testProxyEnv                  = map[string]string{}
	singletonMockInterceptRuleMgr = &mockInterceptRuleMgr{}
)

var mockConf = `{
    "cniVersion": "%s",
	"name": "istio-plugin-sample-test",
	"type": "sample",
    "capabilities": {
        "testCapability": false
    },
    "ipam": {
        "type": "testIPAM"
    },
    "dns": {
        "nameservers": ["testNameServer"],
        "domain": "testDomain",
        "search": ["testSearch"],
        "options": ["testOption"]
    },
    "prevResult": {
        "cniversion": "%s",
        "interfaces": [
            {
                "name": "%s",
                "sandbox": "%s"
            }
        ],
        "ips": [
            {
                "version": "4",
                "address": "10.0.0.2/24",
                "gateway": "10.0.0.1",
                "interface": 0
            }
        ],
        "routes": []

    },
    "log_level": "debug",
    "cni_event_address": "%s",
    "ambient_enabled": %t,
    "kubernetes": {
        "k8s_api_root": "APIRoot",
        "kubeconfig": "testK8sConfig",
		"intercept_type": "%s",
        "node_name": "testNodeName",
        "exclude_namespaces": ["testExcludeNS"],
        "cni_bin_dir": "/testDirectory"
    }
}`

type mockInterceptRuleMgr struct {
	lastRedirect []*Redirect
}

func init() {
	testAnnotations[sidecarStatusKey] = "true"
}

func (mrdir *mockInterceptRuleMgr) Program(podName, netns string, redirect *Redirect) error {
	nsenterFuncCalled = true
	mrdir.lastRedirect = append(mrdir.lastRedirect, redirect)
	return nil
}

func NewMockInterceptRuleMgr() InterceptRuleMgr {
	return singletonMockInterceptRuleMgr
}

func mocknewK8sClient(conf Config) (kubernetes.Interface, error) {
	var cs kubernetes.Clientset

	getKubePodInfoCalled = true

	return &cs, nil
}

func mockgetK8sPodInfo(client kubernetes.Interface, podName, podNamespace string) (*PodInfo, error) {
	pi := PodInfo{}
	pi.Containers = testContainers
	pi.Labels = testLabels
	pi.Annotations = testAnnotations
	pi.ProxyEnvironments = testProxyEnv

	return &pi, nil
}

// FIXME most of this is really unnecessarily stateful and prone to create weird test bugs
func resetGlobalTestVariables() {
	ambientEnabled = false
	getKubePodInfoCalled = false
	nsenterFuncCalled = false
	cniAddServerCalled = false
	testContainers = sets.New("mockContainer", "foo-init")
	testLabels = map[string]string{}
	testAnnotations = map[string]string{}
	testProxyEnv = map[string]string{}

	testAnnotations[sidecarStatusKey] = "true"
	k8Args = fmt.Sprintf("K8S_POD_NAMESPACE=%s;K8S_POD_NAME=%s", testNSName, testPodName)
	newKubeClient = nil
}

// returns the test server URL and a dispose func for the test server
func setupCNIEventClientWithMockServer(serverErr bool) (string, func()) {
	// replace the global CNI client with mock
	newCNIClient = func(address, path string) CNIEventClient {
		c := http.DefaultClient

		eventC := CNIEventClient{
			client: c,
			url:    address + path,
		}
		return eventC
	}

	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		cniAddServerCalled = true
		if serverErr {
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte("server not happy"))
			return
		}
		res.WriteHeader(http.StatusOK)
		res.Write([]byte("server happy"))
	}))

	return testServer.URL, func() { testServer.Close() }
}

func testSetArgs(stdinData string) *skel.CmdArgs {
	return &skel.CmdArgs{
		ContainerID: "testContainerID",
		Netns:       sandboxDirectory,
		IfName:      ifname,
		Args:        k8Args,
		Path:        "/tmp",
		StdinData:   []byte(stdinData),
	}
}

func testCmdInvalidVersion(t *testing.T, f func(args *skel.CmdArgs) error) {
	cniConf := fmt.Sprintf(mockConf, invalidVersion, preVersion, ifname, sandboxDirectory, "", ambientEnabled, "mock")
	args := testSetArgs(cniConf)

	err := f(args)
	if err != nil {
		if !strings.Contains(err.Error(), "cannot convert: no valid IP addresses") {
			t.Fatalf("expected substring error 'cannot convert: no valid IP addresses', got: %v", err)
		}
	} else {
		t.Fatalf("expected failed CNI version, got: no error")
	}
}

func testCmdAddExpectFail(t *testing.T, stdinData string, objects ...runtime.Object) {
	newKubeClient = func(_ Config) (kubernetes.Interface, error) {
		return fake.NewSimpleClientset(objects...), nil
	}

	args := testSetArgs(stdinData)

	_, _, err := testutils.CmdAddWithArgs(
		&skel.CmdArgs{
			Netns:     sandboxDirectory,
			IfName:    ifname,
			StdinData: []byte(stdinData),
		}, func() error { return CmdAdd(args) })
	if err == nil {
		t.Fatalf("expected to fail, but did not!")
	}
}

func testCmdAdd(t *testing.T) {
	cniConf := fmt.Sprintf(mockConf, currentVersion, currentVersion, ifname, sandboxDirectory, "", ambientEnabled, "mock")
	testCmdAddWithStdinData(t, cniConf)
}

func testCmdAddWithStdinData(t *testing.T, stdinData string, objects ...runtime.Object) {
	// FIXME some older sidecar tests don't use the regular mockable kube client and won't pass an array of
	// the expected objects.
	if len(objects) == 0 {
		newKubeClient = mocknewK8sClient
		getKubePodInfo = mockgetK8sPodInfo
	} else {
		newKubeClient = func(_ Config) (kubernetes.Interface, error) {
			return fake.NewSimpleClientset(objects...), nil
		}
	}

	args := testSetArgs(stdinData)

	result, _, err := testutils.CmdAddWithArgs(
		&skel.CmdArgs{
			Netns:     sandboxDirectory,
			IfName:    ifname,
			StdinData: []byte(stdinData),
		}, func() error { return CmdAdd(args) })
	if err != nil {
		t.Fatalf("failed with error: %v", err)
	}

	if result.Version() != cniv1.ImplementedSpecVersion {
		t.Fatalf("failed with invalid version, expected: %v got:%v",
			cniv1.ImplementedSpecVersion, result.Version())
	}
}

// Validate k8sArgs struct works for unmarshalling kubelet args
func TestLoadArgs(t *testing.T) {
	kubeletArgs := "IgnoreUnknown=1;K8S_POD_NAMESPACE=istio-system;" +
		"K8S_POD_NAME=istio-sidecar-injector-8489cf78fb-48pvg;" +
		"K8S_POD_INFRA_CONTAINER_ID=3c41e946cf17a32760ff86940a73b06982f1815e9083cf2f4bfccb9b7605f326"

	k8sArgs := K8sArgs{}
	if err := types.LoadArgs(kubeletArgs, &k8sArgs); err != nil {
		t.Fatalf("LoadArgs failed with error: %v", err)
	}

	if string(k8sArgs.K8S_POD_NAMESPACE) == "" || string(k8sArgs.K8S_POD_NAME) == "" {
		t.Fatalf("LoadArgs didn't convert args properly, K8S_POD_NAME=\"%s\";K8S_POD_NAMESPACE=\"%s\"",
			string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE))
	}
}

func TestCmdAddAmbientEnabledOnNS(t *testing.T) {
	defer resetGlobalTestVariables()

	url, serverClose := setupCNIEventClientWithMockServer(false)
	defer serverClose()

	ambientEnabled = true
	cniConf := fmt.Sprintf(mockConf, currentVersion, currentVersion, ifname, sandboxDirectory, url, ambientEnabled, "mock")

	fakePod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodName,
			Namespace: testNSName,
		},
	}

	fakeNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNSName,
			Namespace: "",
			Labels:    map[string]string{constants.DataplaneMode: constants.DataplaneModeAmbient},
		},
	}

	testCmdAddWithStdinData(t, cniConf, fakePod, fakeNS)

	// Pod in namespace with enabled ambient label, should be added to mesh
	assert.Equal(t, cniAddServerCalled, true)
}

func TestCmdAddAmbientEnabledOnNSServerFails(t *testing.T) {
	defer resetGlobalTestVariables()

	url, serverClose := setupCNIEventClientWithMockServer(true)
	defer serverClose()
	ambientEnabled = true
	cniConf := fmt.Sprintf(mockConf, currentVersion, currentVersion, ifname, sandboxDirectory, url, ambientEnabled, "mock")

	fakePod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodName,
			Namespace: testNSName,
		},
	}

	fakeNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNSName,
			Namespace: "",
			Labels:    map[string]string{constants.DataplaneMode: constants.DataplaneModeAmbient},
		},
	}

	testCmdAddExpectFail(t, cniConf, fakePod, fakeNS)

	// server called, but errored
	assert.Equal(t, cniAddServerCalled, true)
}

func TestCmdAddPodWithProxySidecarAmbientEnabledNS(t *testing.T) {
	defer resetGlobalTestVariables()

	url, serverClose := setupCNIEventClientWithMockServer(false)
	defer serverClose()
	ambientEnabled = true
	cniConf := fmt.Sprintf(mockConf, currentVersion, currentVersion, ifname, sandboxDirectory, url, ambientEnabled, "mock")

	proxy := corev1.Container{Name: "istio-proxy"}
	app := corev1.Container{Name: "app"}
	fakePod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        testPodName,
			Namespace:   testNSName,
			Annotations: map[string]string{annotation.SidecarStatus.Name: "true"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{app, proxy},
		},
	}

	fakeNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNSName,
			Namespace: "",
			Labels:    map[string]string{constants.DataplaneMode: constants.DataplaneModeAmbient},
		},
	}

	testCmdAddWithStdinData(t, cniConf, fakePod, fakeNS)

	// Pod has sidecar annotation from injector, should not be added to mesh
	assert.Equal(t, cniAddServerCalled, false)
}

func TestCmdAddPodWithGenericSidecar(t *testing.T) {
	defer resetGlobalTestVariables()

	url, serverClose := setupCNIEventClientWithMockServer(false)
	defer serverClose()
	ambientEnabled = true
	cniConf := fmt.Sprintf(mockConf, currentVersion, currentVersion, ifname, sandboxDirectory, url, ambientEnabled, "mock")

	proxy := corev1.Container{Name: "istio-proxy"}
	app := corev1.Container{Name: "app"}
	fakePod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodName,
			Namespace: testNSName,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{app, proxy},
		},
	}

	fakeNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNSName,
			Namespace: "",
			Labels:    map[string]string{constants.DataplaneMode: constants.DataplaneModeAmbient},
		},
	}

	testCmdAddWithStdinData(t, cniConf, fakePod, fakeNS)

	// Pod should be added to ambient mesh
	assert.Equal(t, cniAddServerCalled, true)
}

func TestCmdAddPodDisabledAnnotation(t *testing.T) {
	defer resetGlobalTestVariables()

	url, serverClose := setupCNIEventClientWithMockServer(false)
	defer serverClose()
	ambientEnabled = true
	cniConf := fmt.Sprintf(mockConf, currentVersion, currentVersion, ifname, sandboxDirectory, url, ambientEnabled, "mock")

	app := corev1.Container{Name: "app"}
	fakePod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        testPodName,
			Namespace:   testNSName,
			Annotations: map[string]string{constants.DataplaneMode: constants.AmbientRedirectionDisabled},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{app},
		},
	}

	fakeNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNSName,
			Namespace: "",
			Labels:    map[string]string{constants.DataplaneMode: constants.AmbientRedirectionEnabled},
		},
	}

	testCmdAddWithStdinData(t, cniConf, fakePod, fakeNS)

	// Pod has an explicit opt-out annotation, should not be added to ambient mesh
	assert.Equal(t, cniAddServerCalled, false)
}

func TestCmdAddPodEnabledNamespaceDisabled(t *testing.T) {
	defer resetGlobalTestVariables()

	url, serverClose := setupCNIEventClientWithMockServer(false)
	defer serverClose()
	ambientEnabled = true
	cniConf := fmt.Sprintf(mockConf, currentVersion, currentVersion, ifname, sandboxDirectory, url, ambientEnabled, "mock")

	app := corev1.Container{Name: "app"}
	fakePod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        testPodName,
			Namespace:   testNSName,
			Annotations: map[string]string{constants.DataplaneMode: constants.AmbientRedirectionEnabled},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{app},
		},
	}

	fakeNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNSName,
			Namespace: "",
		},
	}

	testCmdAddWithStdinData(t, cniConf, fakePod, fakeNS)

	// Currently, we do not allow individual pod opt-in to ambient if namespace is not labeled, so pod
	// shouls not be added to ambient
	assert.Equal(t, cniAddServerCalled, false)
}

func TestCmdAddPodInExcludedNamespace(t *testing.T) {
	defer resetGlobalTestVariables()

	url, serverClose := setupCNIEventClientWithMockServer(false)
	defer serverClose()
	ambientEnabled = true
	cniConf := fmt.Sprintf(mockConf, currentVersion, currentVersion, ifname, sandboxDirectory, url, ambientEnabled, "mock")

	app := corev1.Container{Name: "app"}
	fakePod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodName,
			Namespace: "testExcludeNS",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{app},
		},
	}

	fakeNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testExcludeNS",
			Namespace: "",
			Labels:    map[string]string{constants.DataplaneMode: constants.AmbientRedirectionEnabled},
		},
	}

	testCmdAddWithStdinData(t, cniConf, fakePod, fakeNS)

	// If the pod is being added to a namespace that is explicitly excluded by plugin config denylist
	// it should never be added, even if the namespace has the annotation
	assert.Equal(t, cniAddServerCalled, false)
}

func TestCmdAdd(t *testing.T) {
	defer resetGlobalTestVariables()

	testCmdAdd(t)
}

func TestCmdAddTwoContainersWithAnnotation(t *testing.T) {
	defer resetGlobalTestVariables()

	testContainers = sets.New("mockContainer", "istio-proxy")
	testAnnotations[injectAnnotationKey] = "false"

	testCmdAdd(t)
}

func TestCmdAddTwoContainersWithLabel(t *testing.T) {
	defer resetGlobalTestVariables()

	testContainers = sets.New("mockContainer", "istio-proxy")
	testAnnotations[label.SidecarInject.Name] = "false"

	testCmdAdd(t)
}

func TestCmdAddTwoContainers(t *testing.T) {
	defer resetGlobalTestVariables()
	testAnnotations[injectAnnotationKey] = "true"
	testContainers = sets.New("mockContainer", "istio-proxy")

	testCmdAdd(t)

	if !nsenterFuncCalled {
		t.Fatalf("expected nsenterFunc to be called")
	}
	mockIntercept, ok := GetInterceptRuleMgrCtor("mock")().(*mockInterceptRuleMgr)
	if !ok {
		t.Fatalf("expect using mockInterceptRuleMgr, actual %v", InterceptRuleMgrTypes["mock"]())
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if r.includeInboundPorts != "*" {
		t.Fatalf("expect includeInboundPorts has value '*' set by istio, actual %v", r.includeInboundPorts)
	}
}

func TestCmdAddTwoContainersWithStarInboundPort(t *testing.T) {
	defer resetGlobalTestVariables()
	testAnnotations[includeInboundPortsKey] = "*"
	testContainers = sets.New("mockContainer", "istio-proxy")
	testCmdAdd(t)

	if !nsenterFuncCalled {
		t.Fatalf("expected nsenterFunc to be called")
	}
	mockIntercept, ok := GetInterceptRuleMgrCtor("mock")().(*mockInterceptRuleMgr)
	if !ok {
		t.Fatalf("expect using mockInterceptRuleMgr, actual %v", InterceptRuleMgrTypes["mock"]())
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if r.includeInboundPorts != "*" {
		t.Fatalf("expect includeInboundPorts is '*', actual %v", r.includeInboundPorts)
	}
}

func TestCmdAddTwoContainersWithEmptyInboundPort(t *testing.T) {
	defer resetGlobalTestVariables()
	delete(testAnnotations, includeInboundPortsKey)
	testContainers = sets.New("mockContainer", "istio-proxy")
	testAnnotations[includeInboundPortsKey] = ""
	testCmdAdd(t)

	if !nsenterFuncCalled {
		t.Fatalf("expected nsenterFunc to be called")
	}
	mockIntercept, ok := GetInterceptRuleMgrCtor("mock")().(*mockInterceptRuleMgr)
	if !ok {
		t.Fatalf("expect using mockInterceptRuleMgr, actual %v", InterceptRuleMgrTypes["mock"])
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if r.includeInboundPorts != "" {
		t.Fatalf("expect includeInboundPorts is \"\", actual %v", r.includeInboundPorts)
	}
}

func TestCmdAddTwoContainersWithEmptyExcludeInboundPort(t *testing.T) {
	defer resetGlobalTestVariables()
	delete(testAnnotations, includeInboundPortsKey)
	testContainers = sets.New("mockContainer", "istio-proxy")
	testAnnotations[excludeInboundPortsKey] = ""
	testCmdAdd(t)

	if !nsenterFuncCalled {
		t.Fatalf("expected nsenterFunc to be called")
	}
	mockIntercept, ok := GetInterceptRuleMgrCtor("mock")().(*mockInterceptRuleMgr)
	if !ok {
		t.Fatalf("expect using mockInterceptRuleMgr, actual %v", InterceptRuleMgrTypes["mock"])
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if r.excludeInboundPorts != "15020,15021,15090" {
		t.Fatalf("expect excludeInboundPorts is \"15090\", actual %v", r.excludeInboundPorts)
	}
}

func TestCmdAddTwoContainersWithExplictExcludeInboundPort(t *testing.T) {
	defer resetGlobalTestVariables()
	delete(testAnnotations, includeInboundPortsKey)
	testContainers = sets.New("mockContainer", "istio-proxy")
	testAnnotations[excludeInboundPortsKey] = "3306"
	testCmdAdd(t)

	if !nsenterFuncCalled {
		t.Fatalf("expected nsenterFunc to be called")
	}
	mockIntercept, ok := GetInterceptRuleMgrCtor("mock")().(*mockInterceptRuleMgr)
	if !ok {
		t.Fatalf("expect using mockInterceptRuleMgr, actual %v", InterceptRuleMgrTypes["mock"])
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if r.excludeInboundPorts != "3306,15020,15021,15090" {
		t.Fatalf("expect excludeInboundPorts is \"3306,15090\", actual %v", r.excludeInboundPorts)
	}
}

func TestCmdAddTwoContainersWithoutSideCar(t *testing.T) {
	defer resetGlobalTestVariables()

	delete(testAnnotations, sidecarStatusKey)
	testContainers = sets.New("mockContainer", "istio-proxy")
	testCmdAdd(t)

	if nsenterFuncCalled {
		t.Fatalf("Didnt Expect nsenterFunc to be called because this pod does not contain a sidecar")
	}
}

func TestCmdAddExcludePod(t *testing.T) {
	defer resetGlobalTestVariables()

	k8Args = "K8S_POD_NAMESPACE=testExcludeNS;K8S_POD_NAME=testPodName"
	getKubePodInfoCalled = false

	testCmdAdd(t)

	if getKubePodInfoCalled {
		t.Fatalf("failed to exclude pod")
	}
}

func TestCmdAddExcludePodWithIstioInitContainer(t *testing.T) {
	defer resetGlobalTestVariables()

	k8Args = "K8S_POD_NAMESPACE=testNS;K8S_POD_NAME=testPodName"
	testContainers = sets.New("mockContainer", "foo-init", "istio-init")
	testAnnotations[sidecarStatusKey] = "true"
	getKubePodInfoCalled = true

	testCmdAdd(t)

	if nsenterFuncCalled {
		t.Fatalf("expected nsenterFunc to not get called")
	}
}

func TestCmdAddExcludePodWithEnvoyDisableEnv(t *testing.T) {
	defer resetGlobalTestVariables()

	k8Args = "K8S_POD_NAMESPACE=testNS;K8S_POD_NAME=testPodName"
	testContainers = sets.New("mockContainer", "istio-proxy", "foo-init")
	testAnnotations[sidecarStatusKey] = "true"
	testProxyEnv["DISABLE_ENVOY"] = "true"
	getKubePodInfoCalled = true

	testCmdAdd(t)

	if nsenterFuncCalled {
		t.Fatalf("expected nsenterFunc to not get called")
	}
}

func TestCmdAddWithKubevirtInterfaces(t *testing.T) {
	defer resetGlobalTestVariables()

	testAnnotations[kubevirtInterfacesKey] = "net1,net2"
	testContainers = sets.New("mockContainer")

	testCmdAdd(t)

	value, ok := testAnnotations[kubevirtInterfacesKey]
	if !ok {
		t.Fatalf("expected kubevirtInterfaces annotation to exist")
	}

	if value != testAnnotations[kubevirtInterfacesKey] {
		t.Fatalf("expected kubevirtInterfaces annotation to equals %s", testAnnotations[kubevirtInterfacesKey])
	}
}

func TestCmdAddWithExcludeInterfaces(t *testing.T) {
	defer resetGlobalTestVariables()

	testAnnotations[excludeInterfacesKey] = "net2"
	testContainers = sets.New("mockContainer")

	testCmdAdd(t)

	value, ok := testAnnotations[excludeInterfacesKey]
	if !ok {
		t.Fatalf("expected excludeInterfaces annotation to exist")
	}

	if value != testAnnotations[excludeInterfacesKey] {
		t.Fatalf("expected excludeInterfaces annotation to equals %s", testAnnotations[excludeInterfacesKey])
	}
}

func TestCmdAddInvalidK8sArgsKeyword(t *testing.T) {
	defer resetGlobalTestVariables()

	k8Args = "K8S_POD_NAMESPACE_InvalidKeyword=istio-system"

	cniConf := fmt.Sprintf(mockConf, currentVersion, currentVersion, ifname, sandboxDirectory, "", ambientEnabled, "mock")
	args := testSetArgs(cniConf)

	err := CmdAdd(args)
	if err != nil {
		if !strings.Contains(err.Error(), "unknown args [\"K8S_POD_NAMESPACE_InvalidKeyword") {
			t.Fatalf(`expected substring "unknown args ["K8S_POD_NAMESPACE_InvalidKeyword, got: %v`, err)
		}
	} else {
		t.Fatalf("expected a failed response for an invalid K8sArgs setting, got: no error")
	}
}

func TestCmdAddInvalidVersion(t *testing.T) {
	defer resetGlobalTestVariables()

	newKubeClient = func(_ Config) (kubernetes.Interface, error) {
		return fake.NewSimpleClientset(), nil
	}
	testCmdInvalidVersion(t, CmdAdd)
}

func TestCmdAddNoPrevResult(t *testing.T) {
	confNoPrevResult := `{
    "cniVersion": "1.0.0",
	"name": "istio-plugin-sample-test",
	"type": "sample",
    "runtimeconfig": {
         "sampleconfig": []
    },
    "loglevel": "debug",
    "kubernetes": {
        "k8sapiroot": "APIRoot",
        "kubeconfig": "testK8sConfig",
        "nodename": "testNodeName",
        "excludenamespaces": "testNS",
        "cnibindir": "/testDirectory"
    }
    }`

	defer resetGlobalTestVariables()
	testCmdAddWithStdinData(t, confNoPrevResult)
}

func TestCmdAddEnableDualStack(t *testing.T) {
	defer resetGlobalTestVariables()
	testProxyEnv["ISTIO_DUAL_STACK"] = "true"
	testContainers = sets.New("mockContainer", "istio-proxy")
	testCmdAdd(t)

	if !nsenterFuncCalled {
		t.Fatalf("expected nsenterFunc to be called")
	}
	mockIntercept, ok := GetInterceptRuleMgrCtor("mock")().(*mockInterceptRuleMgr)
	if !ok {
		t.Fatalf("expect using mockInterceptRuleMgr, actual %v", InterceptRuleMgrTypes["mock"]())
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if !r.dualStack {
		t.Fatalf("expect dualStack is true, actual %v", r.dualStack)
	}
}

func MockInterceptRuleMgrCtor() InterceptRuleMgr {
	return NewMockInterceptRuleMgr()
}

func TestMain(m *testing.M) {
	// call flag.Parse() here if TestMain uses flags
	InterceptRuleMgrTypes["mock"] = MockInterceptRuleMgrCtor

	os.Exit(m.Run())
}

func Test_dedupPorts(t *testing.T) {
	type args struct {
		ports []string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "No duplicates",
			args: args{ports: []string{"1234", "2345"}},
			want: []string{"1234", "2345"},
		},
		{
			name: "Sequential Duplicates",
			args: args{ports: []string{"1234", "1234", "2345", "2345"}},
			want: []string{"1234", "2345"},
		},
		{
			name: "Mixed Duplicates",
			args: args{ports: []string{"1234", "2345", "1234", "2345"}},
			want: []string{"1234", "2345"},
		},
		{
			name: "Empty",
			args: args{ports: []string{}},
			want: []string{},
		},
		{
			name: "Non-parseable",
			args: args{ports: []string{"abcd", "2345", "abcd"}},
			want: []string{"abcd", "2345"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dedupPorts(tt.args.ports); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dedupPorts() = %v, want %v", got, tt.want)
			}
		})
	}
}
