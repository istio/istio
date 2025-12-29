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
	"reflect"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	testPodName          = "testPod"
	testNSName           = "testNS"
	testSandboxDirectory = "/tmp"
	invalidVersion       = "0.1.0"
	preVersion           = "0.2.0"
)

var mockConfTmpl = `{
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
    "plugin_log_level": "debug",
    "cni_agent_run_dir": "%s",
    "ambient_enabled": %t,
	"enablement_selectors": [
		{
			"podSelector": {
				"matchLabels": {
					"istio.io/dataplane-mode": "ambient"
				}
			}
        },
		{
			"podSelector": {
				"matchExpressions": [
					{
						"key": "istio.io/dataplane-mode",
						"operator": "NotIn",
						"values": ["none"]
					}
				]
			},
			"namespaceSelector": {
				"matchLabels": {
					"istio.io/dataplane-mode": "ambient"
				}
			}
		}
	],
	"exclude_namespaces": ["testExcludeNS"],
    "kubernetes": {
        "k8s_api_root": "APIRoot",
        "kubeconfig": "testK8sConfig",
		"intercept_type": "%s"
    }
}`

type mockInterceptRuleMgr struct {
	lastRedirect []*Redirect
}

func buildMockConf(ambientEnabled bool) string {
	return fmt.Sprintf(
		mockConfTmpl,
		"1.0.0",
		"1.0.0",
		"eth0",
		testSandboxDirectory,
		"", // unused here
		ambientEnabled,
		"mock",
	)
}

func buildFakePodAndNSForClient() (*corev1.Pod, *corev1.Namespace) {
	proxy := corev1.Container{Name: "mockContainer"}
	app := corev1.Container{Name: "foo-init"}
	fakePod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        testPodName,
			Namespace:   testNSName,
			Annotations: map[string]string{},
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
			Labels:    map[string]string{},
		},
	}

	return fakePod, fakeNS
}

func (mrdir *mockInterceptRuleMgr) Program(podName, netns string, redirect *Redirect) error {
	mrdir.lastRedirect = append(mrdir.lastRedirect, redirect)
	return nil
}

// returns the test server URL and a dispose func for the test server
func setupCNIEventClientWithMockServer(serverErr bool) func() bool {
	cniAddServerCalled := false

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

	// replace the global CNI client with mock
	newCNIClient = func(address, path string) CNIEventClient {
		c := http.DefaultClient

		eventC := CNIEventClient{
			client: c,
			url:    testServer.URL + path,
		}
		return eventC
	}

	return func() bool {
		testServer.Close()
		return cniAddServerCalled
	}
}

func buildCmdArgs(stdinData, podName, podNamespace string) *skel.CmdArgs {
	return &skel.CmdArgs{
		ContainerID: "testContainerID",
		Netns:       testSandboxDirectory,
		IfName:      "eth0",
		Args:        fmt.Sprintf("K8S_POD_NAMESPACE=%s;K8S_POD_NAME=%s", podNamespace, podName),
		Path:        "/tmp",
		StdinData:   []byte(stdinData),
	}
}

func testCmdAddExpectFail(t *testing.T, stdinData string, objects ...runtime.Object) *mockInterceptRuleMgr {
	args := buildCmdArgs(stdinData, testPodName, testNSName)

	conf, err := parseConfig(args.StdinData)
	if err != nil {
		t.Fatalf("config parse failed with error: %v", err)
	}

	// Create a kube client
	client := kube.NewFakeClient(objects...)

	mockRedir := &mockInterceptRuleMgr{}
	err = doAddRun(args, conf, client.Kube(), mockRedir)
	if err == nil {
		t.Fatal("expected to fail, but did not!")
	}

	return mockRedir
}

func testDoAddRun(t *testing.T, stdinData, nsName string, objects ...runtime.Object) *mockInterceptRuleMgr {
	args := buildCmdArgs(stdinData, testPodName, nsName)

	conf, err := parseConfig(args.StdinData)
	if err != nil {
		t.Fatalf("config parse failed with error: %v", err)
	}

	// Create a kube client
	client := kube.NewFakeClient(objects...)

	mockRedir := &mockInterceptRuleMgr{}
	err = doAddRun(args, conf, client.Kube(), mockRedir)
	if err != nil {
		t.Fatalf("failed with error: %v", err)
	}

	return mockRedir
}

func TestCmdAddAmbientEnabledOnNS(t *testing.T) {
	serverClose := setupCNIEventClientWithMockServer(false)

	cniConf := buildMockConf(true)

	pod, ns := buildFakePodAndNSForClient()
	ns.ObjectMeta.Labels = map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient}

	testDoAddRun(t, cniConf, testNSName, pod, ns)

	wasCalled := serverClose()
	// Pod in namespace with enabled ambient label, should be added to mesh
	assert.Equal(t, wasCalled, true)
}

func TestCmdAddAmbientEnabledOnNSServerFails(t *testing.T) {
	serverClose := setupCNIEventClientWithMockServer(true)

	cniConf := buildMockConf(true)

	pod, ns := buildFakePodAndNSForClient()
	ns.ObjectMeta.Labels = map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient}

	testCmdAddExpectFail(t, cniConf, pod, ns)

	wasCalled := serverClose()
	// server called, but errored
	assert.Equal(t, wasCalled, true)
}

func TestCmdAddPodWithProxySidecarAmbientEnabledNS(t *testing.T) {
	serverClose := setupCNIEventClientWithMockServer(false)

	cniConf := buildMockConf(true)

	pod, ns := buildFakePodAndNSForClient()

	proxy := corev1.Container{Name: "istio-proxy"}
	app := corev1.Container{Name: "app"}

	pod.Spec.Containers = []corev1.Container{app, proxy}
	pod.ObjectMeta.Annotations = map[string]string{annotation.SidecarStatus.Name: "true"}
	ns.ObjectMeta.Labels = map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient}

	testDoAddRun(t, cniConf, testNSName, pod, ns)

	wasCalled := serverClose()
	// Pod has sidecar annotation from injector, should not be added to mesh
	assert.Equal(t, wasCalled, false)
}

func TestCmdAddPodWithGenericSidecar(t *testing.T) {
	serverClose := setupCNIEventClientWithMockServer(false)

	cniConf := buildMockConf(true)

	pod, ns := buildFakePodAndNSForClient()

	proxy := corev1.Container{Name: "istio-proxy"}
	app := corev1.Container{Name: "app"}

	pod.Spec.Containers = []corev1.Container{app, proxy}
	ns.ObjectMeta.Labels = map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient}

	testDoAddRun(t, cniConf, testNSName, pod, ns)

	wasCalled := serverClose()
	// Pod should be added to ambient mesh
	assert.Equal(t, wasCalled, true)
}

func TestCmdAddPodDisabledLabel(t *testing.T) {
	serverClose := setupCNIEventClientWithMockServer(false)

	cniConf := buildMockConf(true)

	pod, ns := buildFakePodAndNSForClient()

	app := corev1.Container{Name: "app"}
	ns.ObjectMeta.Labels = map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient}
	pod.ObjectMeta.Labels = map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone}
	pod.Spec.Containers = []corev1.Container{app}

	testDoAddRun(t, cniConf, testNSName, pod, ns)

	wasCalled := serverClose()
	// Pod has an explicit opt-out label, should not be added to ambient mesh
	assert.Equal(t, wasCalled, false)
}

func TestCmdAddPodEnabledNamespaceDisabled(t *testing.T) {
	serverClose := setupCNIEventClientWithMockServer(false)

	cniConf := buildMockConf(true)

	pod, ns := buildFakePodAndNSForClient()

	app := corev1.Container{Name: "app"}
	pod.ObjectMeta.Labels = map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient}
	pod.Spec.Containers = []corev1.Container{app}

	testDoAddRun(t, cniConf, testNSName, pod, ns)

	wasCalled := serverClose()
	assert.Equal(t, wasCalled, true)
}

func TestCmdAddPodInExcludedNamespace(t *testing.T) {
	serverClose := setupCNIEventClientWithMockServer(false)

	cniConf := buildMockConf(true)

	excludedNS := "testExcludeNS"
	pod, ns := buildFakePodAndNSForClient()

	app := corev1.Container{Name: "app"}
	ns.ObjectMeta.Name = excludedNS
	ns.ObjectMeta.Labels = map[string]string{label.IoIstioDataplaneMode.Name: constants.AmbientRedirectionEnabled}

	pod.ObjectMeta.Namespace = excludedNS
	pod.Spec.Containers = []corev1.Container{app}

	testDoAddRun(t, cniConf, excludedNS, pod, ns)

	wasCalled := serverClose()
	// If the pod is being added to a namespace that is explicitly excluded by plugin config denylist
	// it should never be added, even if the namespace has the annotation
	assert.Equal(t, wasCalled, false)
}

func TestCmdAdd(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()
	testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)
}

func TestCmdAddTwoContainersWithAnnotation(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()

	pod.Spec.Containers[0].Name = "mockContainer"
	pod.Spec.Containers[1].Name = "istio-proxy"
	pod.ObjectMeta.Annotations[injectAnnotationKey] = "false"

	testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)
}

func TestCmdAddTwoContainersWithLabel(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()
	pod.Spec.Containers[0].Name = "mockContainer"
	pod.Spec.Containers[1].Name = "istio-proxy"
	pod.ObjectMeta.Annotations[label.SidecarInject.Name] = "false"

	testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)
}

func TestCmdAddTwoContainers(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()

	pod.Spec.Containers[0].Name = "mockContainer"
	pod.Spec.Containers[1].Name = "istio-proxy"
	pod.ObjectMeta.Annotations[sidecarStatusKey] = "true"

	mockIntercept := testDoAddRun(t, buildMockConf(false), testNSName, pod, ns)

	if len(mockIntercept.lastRedirect) == 0 {
		t.Fatal("expected nsenterFunc to be called")
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if r.includeInboundPorts != "*" {
		t.Fatalf("expect includeInboundPorts has value '*' set by istio, actual %v", r.includeInboundPorts)
	}
}

func TestCmdAddTwoContainersWithStarInboundPort(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()

	pod.Spec.Containers[0].Name = "mockContainer"
	pod.Spec.Containers[1].Name = "istio-proxy"
	pod.ObjectMeta.Annotations[sidecarStatusKey] = "true"
	pod.ObjectMeta.Annotations[includeInboundPortsKey] = "*"

	mockIntercept := testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)

	if len(mockIntercept.lastRedirect) != 1 {
		t.Fatal("expected nsenterFunc to be called")
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if r.includeInboundPorts != "*" {
		t.Fatalf("expect includeInboundPorts is '*', actual %v", r.includeInboundPorts)
	}
}

func TestCmdAddTwoContainersWithEmptyInboundPort(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()

	pod.Spec.Containers[0].Name = "mockContainer"
	pod.Spec.Containers[1].Name = "istio-proxy"
	pod.ObjectMeta.Annotations[sidecarStatusKey] = "true"
	pod.ObjectMeta.Annotations[includeInboundPortsKey] = ""

	mockIntercept := testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)

	if len(mockIntercept.lastRedirect) != 1 {
		t.Fatal("expected nsenterFunc to be called")
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if r.includeInboundPorts != "" {
		t.Fatalf("expect includeInboundPorts is \"\", actual %v", r.includeInboundPorts)
	}
}

func TestCmdAddTwoContainersWithEmptyExcludeInboundPort(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()
	pod.Spec.Containers[0].Name = "mockContainer"
	pod.Spec.Containers[1].Name = "istio-proxy"
	pod.ObjectMeta.Annotations[sidecarStatusKey] = "true"
	pod.ObjectMeta.Annotations[excludeInboundPortsKey] = ""

	mockIntercept := testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)

	if len(mockIntercept.lastRedirect) != 1 {
		t.Fatal("expected nsenterFunc to be called")
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if r.excludeInboundPorts != "15020,15021,15090" {
		t.Fatalf("expect excludeInboundPorts is \"15090\", actual %v", r.excludeInboundPorts)
	}
}

func TestCmdAddTwoContainersWithExplictExcludeInboundPort(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()
	pod.Spec.Containers[0].Name = "mockContainer"
	pod.Spec.Containers[1].Name = "istio-proxy"
	pod.ObjectMeta.Annotations[sidecarStatusKey] = "true"
	pod.ObjectMeta.Annotations[excludeInboundPortsKey] = "3306"

	mockIntercept := testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)

	if len(mockIntercept.lastRedirect) == 0 {
		t.Fatal("expected nsenterFunc to be called")
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if r.excludeInboundPorts != "3306,15020,15021,15090" {
		t.Fatalf("expect excludeInboundPorts is \"3306,15090\", actual %v", r.excludeInboundPorts)
	}
}

func TestCmdAddTwoContainersWithoutSideCar(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()
	pod.Spec.Containers[0].Name = "mockContainer"
	pod.Spec.Containers[1].Name = "istio-proxy"

	mockIntercept := testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)

	if len(mockIntercept.lastRedirect) != 0 {
		t.Fatal("Didn't Expect nsenterFunc to be called because this pod does not contain a sidecar")
	}
}

func TestCmdAddExcludePod(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()

	mockIntercept := testDoAddRun(t, buildMockConf(true), "testExcludeNS", pod, ns)
	if len(mockIntercept.lastRedirect) != 0 {
		t.Fatal("failed to exclude pod")
	}
}

func TestCmdAddExcludePodWithIstioInitContainer(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()
	pod.ObjectMeta.Annotations[sidecarStatusKey] = "true"
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "istio-init"})

	mockIntercept := testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)

	if len(mockIntercept.lastRedirect) != 0 {
		t.Fatal("failed to exclude pod")
	}
}

func TestCmdAddExcludePodWithEnvoyDisableEnv(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()
	pod.ObjectMeta.Annotations[sidecarStatusKey] = "true"
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
		Name: "istio-init",
		Env:  []corev1.EnvVar{{Name: "DISABLE_ENVOY", Value: "true"}},
	})

	mockIntercept := testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)

	if len(mockIntercept.lastRedirect) != 0 {
		t.Fatal("failed to exclude pod")
	}
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
	"ambient_enabled": %t,
	"enablement_selectors": [
		{
			"podSelector": {
				"matchLabels": {
					"istio.io/dataplane-mode": "ambient"
				}
			}
        },
		{
			"podSelector": {
				"matchExpressions": [
					{
						"key": "istio.io/dataplane-mode",
						"operator": "NotIn",
						"values": ["none"]
					}
				]
			},
			"namespaceSelector": {
				"matchLabels": {
					"istio.io/dataplane-mode": "ambient"
				}
			}
		}
	],
    "kubernetes": {
        "k8sapiroot": "APIRoot",
        "kubeconfig": "testK8sConfig",
        "nodename": "testNodeName",
        "excludenamespaces": "testNS",
        "cnibindir": "/testDirectory"
    }
    }`

	pod, ns := buildFakePodAndNSForClient()
	testDoAddRun(t, fmt.Sprintf(confNoPrevResult, false), testNSName, pod, ns)
	testDoAddRun(t, fmt.Sprintf(confNoPrevResult, true), testNSName, pod, ns)
}

func TestCmdAddEnableDualStack(t *testing.T) {
	pod, ns := buildFakePodAndNSForClient()
	pod.ObjectMeta.Annotations[sidecarStatusKey] = "true"
	pod.Spec.Containers = []corev1.Container{
		{
			Name: "istio-proxy",
			Env:  []corev1.EnvVar{{Name: "ISTIO_DUAL_STACK", Value: "true"}},
		}, {Name: "mockContainer"},
	}

	mockIntercept := testDoAddRun(t, buildMockConf(true), testNSName, pod, ns)

	if len(mockIntercept.lastRedirect) == 0 {
		t.Fatal("expected nsenterFunc to be called")
	}
	r := mockIntercept.lastRedirect[len(mockIntercept.lastRedirect)-1]
	if !r.dualStack {
		t.Fatalf("expect dualStack is true, actual %v", r.dualStack)
	}
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
