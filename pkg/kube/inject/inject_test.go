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

package inject

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/google/go-cmp/cmp"
	securityv1 "github.com/openshift/api/security/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/api/annotation"
	meshapi "istio.io/api/mesh/v1alpha1"
	proxyConfig "istio.io/api/networking/v1beta1"
	opconfig "istio.io/istio/operator/pkg/apis"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/kube/multicluster"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/platform"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

type mockInjector struct {
	client kube.Client
}

func (m *mockInjector) Inject(pod *corev1.Pod, namespace string) ([]byte, error) {
	return nil, nil
}

func (m *mockInjector) GetKubeClient() kube.Client {
	return m.client
}

// TestInjection tests both the mutating webhook and kube-inject. It does this by sharing the same input and output
// test files and running through the two different code paths.
func TestInjection(t *testing.T) {
	nodes := []*corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
			},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{
					KubeletVersion: "v1.33.0",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
			},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{
					KubeletVersion: "v1.34.0",
				},
			},
		},
	}

	// Convert nodes to runtime.Object
	var objects []runtime.Object
	for _, node := range nodes {
		objects = append(objects, node)
	}

	objects = append(objects, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Annotations: map[string]string{
				securityv1.UIDRangeAnnotation:           "1000620000/10000",
				securityv1.SupplementalGroupsAnnotation: "1000620000/10000",
			},
		},
	})

	multi := multicluster.NewFakeController()
	client := kube.NewFakeClient(objects...)
	multiclusterNamespaceController := multicluster.BuildMultiClusterKclientComponent[*corev1.Namespace](multi, kubetypes.Filter{})
	multiclusterNodeController := multicluster.BuildMultiClusterKclientComponent[*corev1.Node](multi, kubetypes.Filter{})

	stop := test.NewStop(t)
	multi.Add(constants.DefaultClusterName, client, stop)
	client.RunAndWait(stop)
	injector := &mockInjector{client: client}

	type testCase struct {
		in            string
		want          string
		setFlags      []string
		inFilePath    string
		mesh          func(m *meshapi.MeshConfig)
		skipWebhook   bool
		skipInjection bool
		expectedError string
		expectedLog   string
		setup         func(t test.Failer)
	}
	cases := []testCase{
		// verify cni
		{
			in:   "hello.yaml",
			want: "hello.yaml.cni.injected",
			setFlags: []string{
				"components.cni.enabled=true",
				"values.cni.provider=default",
				"values.global.network=network1",
			},
		},
		{
			in:   "hello.yaml",
			want: "hello.yaml.proxyImageName.injected",
			setFlags: []string{
				"values.global.proxy.image=proxyTest",
			},
		},
		{
			in:   "hello.yaml",
			want: "hello-tproxy.yaml.injected",
			mesh: func(m *meshapi.MeshConfig) {
				m.DefaultConfig.InterceptionMode = meshapi.ProxyConfig_TPROXY
			},
		},
		{
			in:       "hello.yaml",
			want:     "hello-always.yaml.injected",
			setFlags: []string{"values.global.imagePullPolicy=Always"},
		},
		{
			in:       "hello.yaml",
			want:     "hello-never.yaml.injected",
			setFlags: []string{"values.global.imagePullPolicy=Never"},
		},
		{
			in:   "hello.yaml",
			want: "hello-old-version.yaml.injected",
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
		},
		{
			in:   "format-duration.yaml",
			want: "format-duration.yaml.injected",
			mesh: func(m *meshapi.MeshConfig) {
				m.DefaultConfig.DrainDuration = durationpb.New(time.Second * 23)
			},
		},
		{
			// Verifies that parameters are applied properly when no annotations are provided.
			in:   "traffic-params.yaml",
			want: "traffic-params.yaml.injected",
			setFlags: []string{
				`values.global.proxy.includeIPRanges=127.0.0.1/24,10.96.0.1/24`,
				`values.global.proxy.excludeIPRanges=10.96.0.2/24,10.96.0.3/24`,
				`values.global.proxy.excludeInboundPorts=4,5,6`,
				`values.global.proxy.statusPort=0`,
			},
		},
		{
			// Verifies that the status params behave properly.
			in:   "status_params.yaml",
			want: "status_params.yaml.injected",
			setFlags: []string{
				`values.global.proxy.statusPort=123`,
				`values.global.proxy.readinessInitialDelaySeconds=100`,
				`values.global.proxy.readinessPeriodSeconds=200`,
				`values.global.proxy.readinessFailureThreshold=300`,
			},
		},
		{
			// Verifies that the kubevirtInterfaces list are applied properly from parameters..
			in:   "kubevirtInterfaces.yaml",
			want: "kubevirtInterfaces.yaml.injected",
			setFlags: []string{
				`values.global.proxy.statusPort=123`,
				`values.global.proxy.readinessInitialDelaySeconds=100`,
				`values.global.proxy.readinessPeriodSeconds=200`,
				`values.global.proxy.readinessFailureThreshold=300`,
			},
		},
		{
			// Verifies that the kubevirtInterfaces list are applied properly from parameters..
			in:   "reroute-virtual-interfaces.yaml",
			want: "reroute-virtual-interfaces.yaml.injected",
			setFlags: []string{
				`values.global.proxy.statusPort=123`,
				`values.global.proxy.readinessInitialDelaySeconds=100`,
				`values.global.proxy.readinessPeriodSeconds=200`,
				`values.global.proxy.readinessFailureThreshold=300`,
			},
		},
		{
			// Verifies that global.imagePullSecrets are applied properly
			in:         "hello.yaml",
			want:       "hello-image-secrets-in-values.yaml.injected",
			inFilePath: "hello-image-secrets-in-values.iop.yaml",
		},
		{
			// Verifies that global.imagePullSecrets are appended properly
			in:         "hello-image-pull-secret.yaml",
			want:       "hello-multiple-image-secrets.yaml.injected",
			inFilePath: "hello-image-secrets-in-values.iop.yaml",
		},
		{
			// Verifies that global.podDNSSearchNamespaces are applied properly
			in:         "hello.yaml",
			want:       "hello-template-in-values.yaml.injected",
			inFilePath: "hello-template-in-values.iop.yaml",
		},
		{
			// Verifies that global.mountMtlsCerts is applied properly
			in:       "hello.yaml",
			want:     "hello-mount-mtls-certs.yaml.injected",
			setFlags: []string{`values.global.mountMtlsCerts=true`},
		},
		{
			// Verifies that k8s.v1.cni.cncf.io/networks is set to istio-cni when not chained
			in:   "hello.yaml",
			want: "hello-cncf-networks.yaml.injected",
			setFlags: []string{
				`components.cni.enabled=true`,
				`values.cni.provider=multus`,
			},
		},
		{
			// Verifies that istio-cni is appended to k8s.v1.cni.cncf.io/networks flat value if set
			in:   "hello-existing-cncf-networks.yaml",
			want: "hello-existing-cncf-networks.yaml.injected",
			setFlags: []string{
				`components.cni.enabled=true`,
				`values.cni.provider=multus`,
			},
		},
		{
			// Verifies that istio-cni is appended to k8s.v1.cni.cncf.io/networks JSON value
			in:   "hello-existing-cncf-networks-json.yaml",
			want: "hello-existing-cncf-networks-json.yaml.injected",
			setFlags: []string{
				`components.cni.enabled=true`,
				`values.cni.provider=multus`,
			},
		},
		{
			// Verifies that HoldApplicationUntilProxyStarts in MeshConfig puts sidecar in front
			in:   "hello.yaml",
			want: "hello.proxyHoldsApplication.yaml.injected",
			setFlags: []string{
				`values.global.proxy.holdApplicationUntilProxyStarts=true`,
			},
		},
		{
			// Verifies that HoldApplicationUntilProxyStarts in MeshConfig puts sidecar in front
			in:   "hello-probes.yaml",
			want: "hello-probes.proxyHoldsApplication.yaml.injected",
			setFlags: []string{
				`values.global.proxy.holdApplicationUntilProxyStarts=true`,
			},
		},
		{
			// Verifies that HoldApplicationUntilProxyStarts in proxyconfig sets lifecycle hook
			in:   "hello-probes-proxyHoldApplication-ProxyConfig.yaml",
			want: "hello-probes-proxyHoldApplication-ProxyConfig.yaml.injected",
		},
		{
			// Verifies that HoldApplicationUntilProxyStarts=false in proxyconfig 'OR's with MeshConfig setting
			in:   "hello-probes-noProxyHoldApplication-ProxyConfig.yaml",
			want: "hello-probes-noProxyHoldApplication-ProxyConfig.yaml.injected",
			setFlags: []string{
				`values.global.proxy.holdApplicationUntilProxyStarts=true`,
			},
		},
		{
			// A test with no pods is not relevant for webhook
			in:          "hello-service.yaml",
			want:        "hello-service.yaml.injected",
			skipWebhook: true,
		},
		{
			// Cronjob is tricky for webhook test since the spec is different. Since the real code will
			// get a pod anyways, the test isn't too useful for webhook anyways.
			in:          "cronjob.yaml",
			want:        "cronjob.yaml.injected",
			skipWebhook: true,
		},
		{
			in:   "cronjob.yaml",
			want: "cronjob-old-version.yaml.injected",
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
			skipWebhook: true,
		},
		{
			in:   "daemonset.yaml",
			want: "daemonset-old-version.yaml.injected",
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
		},
		{
			in:   "job.yaml",
			want: "job-old-version.yaml.injected",
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
		},
		{
			in:   "pod.yaml",
			want: "pod-old-version.yaml.injected",
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
		},
		{
			in:   "replicaset.yaml",
			want: "replicaset-old-version.yaml.injected",
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
		},
		{
			in:   "statefulset.yaml",
			want: "statefulset-old-version.yaml.injected",
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
		},
		{
			in:            "traffic-annotations-bad-includeipranges.yaml",
			expectedError: "includeipranges",
		},
		{
			in:            "traffic-annotations-bad-excludeipranges.yaml",
			expectedError: "excludeipranges",
		},
		{
			in:            "traffic-annotations-bad-includeinboundports.yaml",
			expectedError: "includeinboundports",
		},
		{
			in:            "traffic-annotations-bad-excludeinboundports.yaml",
			expectedError: "excludeinboundports",
		},
		{
			in:            "traffic-annotations-bad-excludeoutboundports.yaml",
			expectedError: "excludeoutboundports",
		},
		{
			in:   "traffic-annotations.yaml",
			want: "traffic-annotations.yaml.injected",
			mesh: func(m *meshapi.MeshConfig) {
				if m.DefaultConfig.ProxyMetadata == nil {
					m.DefaultConfig.ProxyMetadata = map[string]string{}
				}
				m.DefaultConfig.ProxyMetadata["ISTIO_META_TLS_CLIENT_KEY"] = "/etc/identity/client/keys/client-key.pem"
			},
		},
		{
			in:   "proxy-override.yaml",
			want: "proxy-override.yaml.injected",
		},
		{
			in:   "explicit-security-context.yaml",
			want: "explicit-security-context.yaml.injected",
		},
		{
			in:   "only-proxy-container.yaml",
			want: "only-proxy-container.yaml.injected",
		},
		{
			in:   "proxy-override-args.yaml",
			want: "proxy-override-args.yaml.injected",
		},
		{
			in:   "proxy-override-runas.yaml",
			want: "proxy-override-runas.yaml.injected",
		},
		{
			in:   "proxy-override-runas.yaml",
			want: "proxy-override-runas.yaml.cni.injected",
			setFlags: []string{
				"components.cni.enabled=true",
			},
		},
		{
			in:   "proxy-override-runas.yaml",
			want: "proxy-override-runas.yaml.tproxy.injected",
			mesh: func(m *meshapi.MeshConfig) {
				m.DefaultConfig.InterceptionMode = meshapi.ProxyConfig_TPROXY
			},
		},
		{
			in:   "proxy-override-args.yaml",
			want: "proxy-override-args-native.yaml.injected",
		},
		{
			in:   "gateway.yaml",
			want: "gateway.yaml.injected",
		},
		{
			in:   "gateway.yaml",
			want: "gateway.yaml.injected",
		},
		{
			in:   "native-sidecar.yaml",
			want: "native-sidecar.yaml.injected",
		},
		{
			in:   "native-sidecar.yaml",
			want: "native-sidecar-old-version.yaml.injected",
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
		},
		{
			in:   "native-sidecar-opt-in.yaml",
			want: "native-sidecar-opt-in.yaml.injected",
		},
		{
			in:   "native-sidecar-opt-in.yaml",
			want: "native-sidecar-opt-in.yaml.injected",
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
		},
		{
			in:   "native-sidecar-opt-out.yaml",
			want: "native-sidecar-opt-out.yaml.injected",
		},
		{
			in:   "native-sidecar-opt-out.yaml",
			want: "native-sidecar-opt-out.yaml.injected",
			setup: func(t test.Failer) {
				test.SetForTest(t, &features.EnableNativeSidecars, false)
			},
		},
		{
			in:         "custom-template.yaml",
			want:       "custom-template.yaml.injected",
			inFilePath: "custom-template.iop.yaml",
		},
		{
			in:   "tcp-probes.yaml",
			want: "tcp-probes.yaml.injected",
		},
		{
			in:          "hello-host-network-with-ns.yaml",
			want:        "hello-host-network-with-ns.yaml.injected",
			expectedLog: "Skipping injection because Deployment \"sample/hello-host-network\" has host networking enabled",
		},
		{
			// Verifies ISTIO_KUBE_APP_PROBERS are correctly merged during multiple injections.
			in:   "merge-probers.yaml",
			want: "merge-probers.yaml.injected",
			setFlags: []string{
				`values.global.proxy.holdApplicationUntilProxyStarts=true`,
			},
		},
		{
			in:   "hello-tracing-disabled.yaml",
			want: "hello-tracing-disabled.yaml.injected",
			mesh: func(m *meshapi.MeshConfig) {
				m.DefaultConfig.Tracing = &meshapi.Tracing{}
			},
		},
		{
			in:   "truncate-canonical-name-pod.yaml",
			want: "truncate-canonical-name-pod.yaml.injected",
		},
		{
			in:   "truncate-canonical-name-custom-controller-pod.yaml",
			want: "truncate-canonical-name-custom-controller-pod.yaml.injected",
		},
		{
			// Test injection on OpenShift. Currently kube-inject does not work, only test webhook
			in:   "hello-openshift.yaml",
			want: "hello-openshift.yaml.injected",
			setFlags: []string{
				"components.cni.enabled=true",
			},
			skipInjection: true,
			setup: func(t test.Failer) {
				test.SetEnvForTest(t, platform.Platform.Name, platform.OpenShift)
			},
		},
		{
			// Test webhook custom injection on OpenShift.
			in:   "hello-openshift-custom-injection.yaml",
			want: "hello-openshift-custom-injection.yaml.injected",
			setFlags: []string{
				"components.cni.enabled=true",
			},
			skipInjection: true,
			setup: func(t test.Failer) {
				test.SetEnvForTest(t, platform.Platform.Name, platform.OpenShift)
			},
		},
		{
			in:   "hello-openshift-tproxy.yaml",
			want: "hello-openshift-tproxy.yaml.injected",
			setFlags: []string{
				"components.cni.enabled=true",
			},
			skipInjection: true,
			setup: func(t test.Failer) {
				test.SetEnvForTest(t, platform.Platform.Name, platform.OpenShift)
			},
		},
		{
			// Validates localhost probes get injected correctly
			in:   "hello-probes-localhost.yaml",
			want: "hello-probes-localhost.yaml.injected",
			mesh: func(m *meshapi.MeshConfig) {
				m.InboundTrafficPolicy = &meshapi.MeshConfig_InboundTrafficPolicy{
					Mode: meshapi.MeshConfig_InboundTrafficPolicy_LOCALHOST,
				}
			},
		},
		{
			in:         "sidecar-spire.yaml",
			want:       "sidecar-spire.yaml.injected",
			inFilePath: "spire-template.iop.yaml",
		},
		{
			in:         "gateway-spire.yaml",
			want:       "gateway-spire.yaml.injected",
			inFilePath: "spire-template.iop.yaml",
		},
	}
	// Keep track of tests we add options above
	// We will search for all test files and skip these ones
	alreadyTested := sets.New[string]()
	for _, t := range cases {
		if t.want != "" {
			alreadyTested.Insert(t.want)
		} else {
			alreadyTested.Insert(t.in + ".injected")
		}
	}
	files, err := os.ReadDir("testdata/inject")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 {
		t.Fatal("Didn't find test files - something must have gone wrong")
	}
	// Automatically add any other test files in the folder. This ensures we don't
	// forget to add to this list, that we don't have duplicates, etc
	// Keep track of all golden files so we can ensure we don't have unused ones later
	allOutputFiles := sets.New[string]()
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".injected") {
			allOutputFiles.Insert(f.Name())
		}
		if strings.HasSuffix(f.Name(), ".iop.yaml") {
			continue
		}
		if !strings.HasSuffix(f.Name(), ".yaml") {
			continue
		}
		want := f.Name() + ".injected"
		if alreadyTested.Contains(want) {
			continue
		}
		cases = append(cases, testCase{in: f.Name(), want: want})
	}

	// Preload default settings. Computation here is expensive, so this speeds the tests up substantially
	defaultTemplate, defaultValues, defaultMesh := getInjectionSettings(t, nil, "")
	for i, c := range cases {
		testName := fmt.Sprintf("[%02d] %s", i, c.want)
		if c.expectedError != "" {
			testName = fmt.Sprintf("[%02d] %s", i, c.in)
		}
		t.Run(testName, func(t *testing.T) {
			if c.setup != nil {
				c.setup(t)
			} else {
				// Tests with custom setup modify global state and cannot run in parallel
				t.Parallel()
			}

			mc, err := mesh.DeepCopyMeshConfig(defaultMesh)
			if err != nil {
				t.Fatal(err)
			}
			sidecarTemplate, valuesConfig := defaultTemplate, defaultValues
			if c.setFlags != nil || c.inFilePath != "" {
				sidecarTemplate, valuesConfig, mc = getInjectionSettings(t, c.setFlags, c.inFilePath)
			}
			if c.mesh != nil {
				c.mesh(mc)
			}

			inputFilePath := "testdata/inject/" + c.in
			wantFilePath := "testdata/inject/" + c.want
			in, err := os.Open(inputFilePath)
			if err != nil {
				t.Fatalf("Failed to open %q: %v", inputFilePath, err)
			}
			t.Cleanup(func() {
				_ = in.Close()
			})

			// First we test kube-inject. This will run exactly what kube-inject does, and write output to the golden files
			t.Run("kube-inject", func(t *testing.T) {
				if c.skipInjection {
					return
				}

				var got bytes.Buffer
				logs := make([]string, 0)
				warn := func(s string) {
					logs = append(logs, s)
					t.Log(s)
				}
				if err = IntoResourceFile(injector, sidecarTemplate.Templates, valuesConfig, "", mc, in, &got, warn); err != nil {
					if c.expectedError != "" {
						if !strings.Contains(strings.ToLower(err.Error()), c.expectedError) {
							t.Fatalf("expected error %q got %q", c.expectedError, err)
						}
						return
					}
					t.Fatalf("IntoResourceFile(%v) returned an error: %v", inputFilePath, err)
				}
				if c.expectedError != "" {
					t.Fatal("expected error but got none")
				}
				if c.expectedLog != "" {
					hasExpectedLog := false
					for _, log := range logs {
						if strings.Contains(log, c.expectedLog) {
							hasExpectedLog = true
							break
						}
					}
					if !hasExpectedLog {
						t.Fatal("expected log but got none")
					}
				}

				// The version string is a maintenance pain for this test. Strip the version string before comparing.
				gotBytes := util.StripVersion(got.Bytes())
				wantBytes := util.ReadGoldenFile(t, gotBytes, wantFilePath)

				util.CompareBytes(t, gotBytes, wantBytes, wantFilePath)
			})

			// Exit early if we don't need to test webhook. We can skip errors since its redundant
			// and painful to test here.
			if c.expectedError != "" || c.skipWebhook {
				return
			}
			// Next run the webhook test. This one is a bit trickier as the webhook operates
			// on Pods, but the inputs are Deployments/StatefulSets/etc. As a result, we need
			// to convert these to pods, then run the injection. This test will *not*
			// overwrite golden files, as we do not have identical textual output as
			// kube-inject. Instead, we just compare the desired/actual pod specs.
			t.Run("webhook", func(t *testing.T) {
				env := &model.Environment{}
				env.SetPushContext(&model.PushContext{
					ProxyConfigs: &model.ProxyConfigs{},
				})

				webhook := &Webhook{
					Config:       sidecarTemplate,
					meshConfig:   mc,
					env:          env,
					valuesConfig: valuesConfig,
					revision:     "default",
					namespaces:   multiclusterNamespaceController,
					nodes:        multiclusterNodeController,
				}

				// Split multi-part yaml documents. Input and output will have the same number of parts.
				inputYAMLs := splitYamlFile(inputFilePath, t)
				wantYAMLs := splitYamlFile(wantFilePath, t)
				for i := 0; i < len(inputYAMLs); i++ {
					t.Run(fmt.Sprintf("yamlPart[%d]", i), func(t *testing.T) {
						runWebhook(t, webhook, inputYAMLs[i], wantYAMLs[i], true)
					})
				}
			})
		})
	}

	// Make sure we don't have any stale test data leftover, as it can cause confusion.
	for _, c := range cases {
		delete(allOutputFiles, c.want)
	}
	if len(allOutputFiles) != 0 {
		t.Fatalf("stale golden files found: %v", allOutputFiles.UnsortedList())
	}
}

func TestInjectionOlderVersion(t *testing.T) {
	nodes := []*corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
			},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{
					KubeletVersion: "v1.33.0",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
			},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{
					KubeletVersion: "v1.28.0",
				},
			},
		},
	}

	// Convert nodes to runtime.Object
	var objects []runtime.Object
	for _, node := range nodes {
		objects = append(objects, node)
	}

	objects = append(objects, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Annotations: map[string]string{
				securityv1.UIDRangeAnnotation:           "1000620000/10000",
				securityv1.SupplementalGroupsAnnotation: "1000620000/10000",
			},
		},
	})

	multi := multicluster.NewFakeController()
	client := kube.NewFakeClient(objects...)
	multiclusterNamespaceController := multicluster.BuildMultiClusterKclientComponent[*corev1.Namespace](multi, kubetypes.Filter{})
	multiclusterNodeController := multicluster.BuildMultiClusterKclientComponent[*corev1.Node](multi, kubetypes.Filter{})

	stop := test.NewStop(t)
	multi.Add(constants.DefaultClusterName, client, stop)
	client.RunAndWait(stop)
	injector := &mockInjector{client: client}

	type testCase struct {
		in            string
		want          string
		setFlags      []string
		inFilePath    string
		mesh          func(m *meshapi.MeshConfig)
		skipWebhook   bool
		skipInjection bool
		expectedError string
		expectedLog   string
		setup         func(t test.Failer)
	}

	// Test cases where an older version of nodes keep istio-proxy as a container
	cases := []testCase{
		{
			in:   "native-sidecar.yaml",
			want: "native-sidecar-old-version.yaml.injected",
		},
		{
			in:          "cronjob.yaml",
			want:        "cronjob-old-version.yaml.injected",
			skipWebhook: true,
		},
		{
			in:   "daemonset.yaml",
			want: "daemonset-old-version.yaml.injected",
		},
		{
			in:   "job.yaml",
			want: "job-old-version.yaml.injected",
		},
		{
			in:   "pod.yaml",
			want: "pod-old-version.yaml.injected",
		},
		{
			in:   "replicaset.yaml",
			want: "replicaset-old-version.yaml.injected",
		},
		{
			in:   "statefulset.yaml",
			want: "statefulset-old-version.yaml.injected",
		},
		{
			in:   "hello.yaml",
			want: "hello-old-version.yaml.injected",
		},
	}

	defaultTemplate, defaultValues, defaultMesh := getInjectionSettings(t, nil, "")
	for i, c := range cases {
		testName := fmt.Sprintf("[%02d] %s", i, c.want)
		if c.expectedError != "" {
			testName = fmt.Sprintf("[%02d] %s", i, c.in)
		}
		t.Run(testName, func(t *testing.T) {
			if c.setup != nil {
				c.setup(t)
			} else {
				// Tests with custom setup modify global state and cannot run in parallel
				t.Parallel()
			}

			mc, err := mesh.DeepCopyMeshConfig(defaultMesh)
			if err != nil {
				t.Fatal(err)
			}
			sidecarTemplate, valuesConfig := defaultTemplate, defaultValues
			if c.setFlags != nil || c.inFilePath != "" {
				sidecarTemplate, valuesConfig, mc = getInjectionSettings(t, c.setFlags, c.inFilePath)
			}
			if c.mesh != nil {
				c.mesh(mc)
			}

			inputFilePath := "testdata/inject/" + c.in
			wantFilePath := "testdata/inject/" + c.want
			in, err := os.Open(inputFilePath)
			if err != nil {
				t.Fatalf("Failed to open %q: %v", inputFilePath, err)
			}
			t.Cleanup(func() {
				_ = in.Close()
			})
			t.Run("kube-inject", func(t *testing.T) {
				if c.skipInjection {
					return
				}

				var got bytes.Buffer
				logs := make([]string, 0)
				warn := func(s string) {
					logs = append(logs, s)
					t.Log(s)
				}
				if err = IntoResourceFile(injector, sidecarTemplate.Templates, valuesConfig, "", mc, in, &got, warn); err != nil {
					if c.expectedError != "" {
						if !strings.Contains(strings.ToLower(err.Error()), c.expectedError) {
							t.Fatalf("expected error %q got %q", c.expectedError, err)
						}
						return
					}
					t.Fatalf("IntoResourceFile(%v) returned an error: %v", inputFilePath, err)
				}
				if c.expectedError != "" {
					t.Fatal("expected error but got none")
				}
				if c.expectedLog != "" {
					hasExpectedLog := false
					for _, log := range logs {
						if strings.Contains(log, c.expectedLog) {
							hasExpectedLog = true
							break
						}
					}
					if !hasExpectedLog {
						t.Fatal("expected log but got none")
					}
				}

				// The version string is a maintenance pain for this test. Strip the version string before comparing.
				gotBytes := util.StripVersion(got.Bytes())
				wantBytes := util.ReadGoldenFile(t, gotBytes, wantFilePath)

				util.CompareBytes(t, gotBytes, wantBytes, wantFilePath)
			})

			// Exit early if we don't need to test webhook. We can skip errors since its redundant
			// and painful to test here.
			if c.expectedError != "" || c.skipWebhook {
				return
			}
			// Next run the webhook test. This one is a bit trickier as the webhook operates
			// on Pods, but the inputs are Deployments/StatefulSets/etc. As a result, we need
			// to convert these to pods, then run the injection. This test will *not*
			// overwrite golden files, as we do not have identical textual output as
			// kube-inject. Instead, we just compare the desired/actual pod specs.
			t.Run("webhook", func(t *testing.T) {
				env := &model.Environment{}
				env.SetPushContext(&model.PushContext{
					ProxyConfigs: &model.ProxyConfigs{},
				})

				webhook := &Webhook{
					Config:       sidecarTemplate,
					meshConfig:   mc,
					env:          env,
					valuesConfig: valuesConfig,
					revision:     "default",
					namespaces:   multiclusterNamespaceController,
					nodes:        multiclusterNodeController,
				}

				// Split multi-part yaml documents. Input and output will have the same number of parts.
				inputYAMLs := splitYamlFile(inputFilePath, t)
				wantYAMLs := splitYamlFile(wantFilePath, t)
				for i := 0; i < len(inputYAMLs); i++ {
					t.Run(fmt.Sprintf("yamlPart[%d]", i), func(t *testing.T) {
						runWebhook(t, webhook, inputYAMLs[i], wantYAMLs[i], true)
					})
				}
			})
		})
	}
}

func testInjectionTemplate(t *testing.T, template, input, expected string) {
	t.Helper()
	tmpl, err := ParseTemplates(map[string]string{SidecarTemplateName: template})
	if err != nil {
		t.Fatal(err)
	}
	env := &model.Environment{}
	env.SetPushContext(&model.PushContext{
		ProxyConfigs: &model.ProxyConfigs{},
	})
	vc, err := NewValuesConfig("{}")
	assert.NoError(t, err)
	webhook := &Webhook{
		Config: &Config{
			Templates:        tmpl,
			Policy:           InjectionPolicyEnabled,
			DefaultTemplates: []string{SidecarTemplateName},
		},
		env:          env,
		valuesConfig: vc,
	}
	runWebhook(t, webhook, []byte(input), []byte(expected), false)
}

func TestMultipleInjectionTemplates(t *testing.T) {
	p, err := ParseTemplates(map[string]string{
		"sidecar": `
spec:
  containers:
  - name: istio-proxy
    image: proxy
`,
		"init": `
spec:
 initContainers:
 - name: istio-init
   image: proxy
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	env := &model.Environment{}
	env.SetPushContext(&model.PushContext{
		ProxyConfigs: &model.ProxyConfigs{},
	})
	vc, err := NewValuesConfig("{}")
	assert.NoError(t, err)
	webhook := &Webhook{
		Config: &Config{
			Templates: p,
			Aliases:   map[string][]string{"both": {"sidecar", "init"}},
			Policy:    InjectionPolicyEnabled,
		},
		valuesConfig: vc,
		env:          env,
	}

	input := `
apiVersion: v1
kind: Pod
metadata:
  name: hello
  annotations:
    inject.istio.io/templates: sidecar,init
spec:
  containers:
  - name: hello
    image: "fake.docker.io/google-samples/hello-go-gke:1.0"
`
	inputAlias := `
apiVersion: v1
kind: Pod
metadata:
  name: hello
  annotations:
    inject.istio.io/templates: both
spec:
  containers:
  - name: hello
    image: "fake.docker.io/google-samples/hello-go-gke:1.0"
`
	// nolint: lll
	expected := `
apiVersion: v1
kind: Pod
metadata:
  annotations:
    inject.istio.io/templates: %s
    prometheus.io/path: /stats/prometheus
    prometheus.io/port: "0"
    prometheus.io/scrape: "true"
    sidecar.istio.io/status: '{"version":"","initContainers":["istio-init"],"containers":["istio-proxy"],"volumes":["istio-envoy","istio-data","istio-podinfo","istio-token","istiod-ca-cert"],"imagePullSecrets":null}'
  name: hello
spec:
  initContainers:
  - name: istio-init
    image: proxy
  containers:
    - name: hello
      image: fake.docker.io/google-samples/hello-go-gke:1.0
    - name: istio-proxy
      image: proxy
`
	runWebhook(t, webhook, []byte(input), []byte(fmt.Sprintf(expected, "sidecar,init")), false)
	runWebhook(t, webhook, []byte(inputAlias), []byte(fmt.Sprintf(expected, "both")), false)
}

// TestStrategicMerge ensures we can use https://github.com/kubernetes/community/blob/master/contributors/devel/sig-api-machinery/strategic-merge-patch.md
// directives in the injection template
func TestStrategicMerge(t *testing.T) {
	testInjectionTemplate(t,
		`
metadata:
  labels:
    $patch: replace
    foo: bar
spec:
  containers:
  - name: injected
    image: "fake.docker.io/google-samples/hello-go-gke:1.1"
`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: hello
  labels:
    key: value
spec:
  containers:
  - name: hello
    image: "fake.docker.io/google-samples/hello-go-gke:1.0"
`,

		// We expect resources to only have limits, since we had the "replace" directive.
		// nolint: lll
		`
apiVersion: v1
kind: Pod
metadata:
  annotations:
    prometheus.io/path: /stats/prometheus
    prometheus.io/port: "0"
    prometheus.io/scrape: "true"
  labels:
    foo: bar
  name: hello
spec:
  containers:
  - name: injected
    image: "fake.docker.io/google-samples/hello-go-gke:1.1"
  - name: hello
    image: "fake.docker.io/google-samples/hello-go-gke:1.0"
`)
}

func runWebhook(t *testing.T, webhook *Webhook, inputYAML []byte, wantYAML []byte, idempotencyCheck bool) {
	// Convert the input YAML to a deployment.
	inputRaw, err := FromRawToObject(inputYAML)
	if err != nil {
		t.Fatal(err)
	}
	inputPod := objectToPod(t, inputRaw)

	// Convert the wanted YAML to a deployment.
	wantRaw, err := FromRawToObject(wantYAML)
	if err != nil {
		t.Fatal(err)
	}
	wantPod := objectToPod(t, wantRaw)

	// Generate the patch.  At runtime, the webhook would actually generate the patch against the
	// pod configuration. But since our input files are deployments, rather than actual pod instances,
	// we have to apply the patch to the template portion of the deployment only.
	templateJSON := convertToJSON(inputPod, t)
	got := webhook.inject(&kube.AdmissionReview{
		Request: &kube.AdmissionRequest{
			Object: runtime.RawExtension{
				Raw: templateJSON,
			},
			Namespace: jsonToUnstructured(inputYAML, t).GetNamespace(),
		},
	}, "")
	var gotPod *corev1.Pod
	// Apply the generated patch to the template.
	if got.Patch != nil {
		patchedPod := &corev1.Pod{}
		patch := prettyJSON(got.Patch, t)
		patchedTemplateJSON := applyJSONPatch(templateJSON, patch, t)
		if err := json.Unmarshal(patchedTemplateJSON, patchedPod); err != nil {
			t.Fatal(err)
		}
		gotPod = patchedPod
	} else {
		gotPod = inputPod
	}

	if err := normalizeAndCompareDeployments(gotPod, wantPod, false, t); err != nil {
		t.Fatal(err)
	}
	if idempotencyCheck {
		t.Run("idempotency", func(t *testing.T) {
			if err := normalizeAndCompareDeployments(gotPod, wantPod, true, t); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSkipUDPPorts(t *testing.T) {
	cases := []struct {
		c     corev1.Container
		ports []string
	}{
		{
			c: corev1.Container{
				Ports: []corev1.ContainerPort{},
			},
		},
		{
			c: corev1.Container{
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 80,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						ContainerPort: 8080,
						Protocol:      corev1.ProtocolTCP,
					},
				},
			},
			ports: []string{"80", "8080"},
		},
		{
			c: corev1.Container{
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 53,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						ContainerPort: 53,
						Protocol:      corev1.ProtocolUDP,
					},
				},
			},
			ports: []string{"53"},
		},
		{
			c: corev1.Container{
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 80,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						ContainerPort: 53,
						Protocol:      corev1.ProtocolUDP,
					},
				},
			},
			ports: []string{"80"},
		},
		{
			c: corev1.Container{
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 53,
						Protocol:      corev1.ProtocolUDP,
					},
				},
			},
		},
	}
	for i := range cases {
		expectPorts := cases[i].ports
		ports := getPortsForContainer(cases[i].c)
		if len(ports) != len(expectPorts) {
			t.Fatalf("unexpected ports result for case %d", i)
		}
		for j := 0; j < len(ports); j++ {
			if ports[j] != expectPorts[j] {
				t.Fatalf("unexpected ports result for case %d: expect %v, got %v", i, expectPorts, ports)
			}
		}
	}
}

func TestCleanProxyConfig(t *testing.T) {
	overrides := mesh.DefaultProxyConfig()
	overrides.ConfigPath = "/foo/bar"
	overrides.DrainDuration = durationpb.New(7 * time.Second)
	overrides.ProxyMetadata = map[string]string{
		"foo": "barr",
	}
	explicit := mesh.DefaultProxyConfig()
	explicit.ConfigPath = constants.ConfigPathDir
	explicit.DrainDuration = durationpb.New(45 * time.Second)
	cases := []struct {
		name   string
		proxy  *meshapi.ProxyConfig
		expect string
	}{
		{
			"default",
			mesh.DefaultProxyConfig(),
			`{}`,
		},
		{
			"explicit default",
			explicit,
			`{}`,
		},
		{
			"overrides",
			overrides,
			`{"configPath":"/foo/bar","drainDuration":"7s","proxyMetadata":{"foo":"barr"}}`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := protoToJSON(tt.proxy)
			if got != tt.expect {
				t.Fatalf("incorrect output: got %v, expected %v", got, tt.expect)
			}
			roundTrip, err := mesh.ApplyProxyConfig(got, mesh.DefaultMeshConfig())
			if err != nil {
				t.Fatal(err)
			}
			if !cmp.Equal(roundTrip.GetDefaultConfig(), tt.proxy, protocmp.Transform()) {
				t.Fatalf("round trip is not identical: got \n%+v, expected \n%+v", *roundTrip.GetDefaultConfig(), tt.proxy)
			}
		})
	}
}

func TestAppendMultusNetwork(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty",
			in:   "",
			want: "istio-cni",
		},
		{
			name: "flat-single",
			in:   "macvlan-conf-1",
			want: "macvlan-conf-1, istio-cni",
		},
		{
			name: "flat-multiple",
			in:   "macvlan-conf-1, macvlan-conf-2",
			want: "macvlan-conf-1, macvlan-conf-2, istio-cni",
		},
		{
			name: "json-single",
			in:   `[{"name": "macvlan-conf-1"}]`,
			want: `[{"name": "macvlan-conf-1"}, {"name": "istio-cni"}]`,
		},
		{
			name: "json-multiple",
			in:   `[{"name": "macvlan-conf-1"}, {"name": "macvlan-conf-2"}]`,
			want: `[{"name": "macvlan-conf-1"}, {"name": "macvlan-conf-2"}, {"name": "istio-cni"}]`,
		},
		{
			name: "json-multiline",
			in: `[
                   {"name": "macvlan-conf-1"},
                   {"name": "macvlan-conf-2"}
                   ]`,
			want: `[
                   {"name": "macvlan-conf-1"},
                   {"name": "macvlan-conf-2"}
                   , {"name": "istio-cni"}]`,
		},
		{
			name: "json-multiline-additional-fields",
			in: `[
                   {"name": "macvlan-conf-1", "another-field": "another-value"},
                   {"name": "macvlan-conf-2"}
                   ]`,
			want: `[
                   {"name": "macvlan-conf-1", "another-field": "another-value"},
                   {"name": "macvlan-conf-2"}
                   , {"name": "istio-cni"}]`,
		},
		{
			name: "json-preconfigured-istio-cni",
			in: `[
                   {"name": "macvlan-conf-1"},
                   {"name": "macvlan-conf-2"},
                   {"name": "istio-cni", "config": "additional-config"},
                   ]`,
			want: `[
                   {"name": "macvlan-conf-1"},
                   {"name": "macvlan-conf-2"},
                   {"name": "istio-cni", "config": "additional-config"},
                   ]`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			actual := appendMultusNetwork(tc.in, "istio-cni")
			if actual != tc.want {
				t.Fatalf("Unexpected result.\nExpected:\n%v\nActual:\n%v", tc.want, actual)
			}
			t.Run("idempotency", func(t *testing.T) {
				actual := appendMultusNetwork(actual, "istio-cni")
				if actual != tc.want {
					t.Fatalf("Function is not idempotent.\nExpected:\n%v\nActual:\n%v", tc.want, actual)
				}
			})
		})
	}
}

func Test_updateClusterEnvs(t *testing.T) {
	type args struct {
		container *corev1.Container
		newKVs    map[string]string
	}
	tests := []struct {
		name string
		args args
		want *corev1.Container
	}{
		{
			args: args{
				container: &corev1.Container{},
				newKVs:    parseInjectEnvs("/inject/net/network1/cluster/cluster1"),
			},
			want: &corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name:  "ISTIO_META_CLUSTER_ID",
						Value: "cluster1",
					},
					{
						Name:  "ISTIO_META_NETWORK",
						Value: "network1",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateClusterEnvs(tt.args.container, tt.args.newKVs)
			if !cmp.Equal(tt.args.container.Env, tt.want.Env) {
				t.Fatalf("updateClusterEnvs got \n%+v, expected \n%+v", tt.args.container.Env, tt.want.Env)
			}
		})
	}
}

func TestProxyImage(t *testing.T) {
	val := func(hub string, tag any) *opconfig.Values {
		t, _ := structpb.NewValue(tag)
		return &opconfig.Values{
			Global: &opconfig.GlobalConfig{
				Hub: hub,
				Tag: t,
			},
		}
	}
	pc := func(imageType string) *proxyConfig.ProxyImage {
		return &proxyConfig.ProxyImage{
			ImageType: imageType,
		}
	}

	ann := func(imageType string) map[string]string {
		if imageType == "" {
			return nil
		}
		return map[string]string{
			annotation.SidecarProxyImageType.Name: imageType,
		}
	}

	for _, tt := range []struct {
		desc string
		v    *opconfig.Values
		pc   *proxyConfig.ProxyImage
		ann  map[string]string
		want string
	}{
		{
			desc: "vals-only-int-tag",
			v:    val("docker.io/istio", 11),
			want: "docker.io/istio/proxyv2:11",
		},
		{
			desc: "pc overrides imageType - float tag",
			v:    val("docker.io/istio", 1.12),
			pc:   pc("distroless"),
			want: "docker.io/istio/proxyv2:1.12-distroless",
		},
		{
			desc: "annotation overrides imageType",
			v:    val("gcr.io/gke-release/asm", "1.11.2-asm.17"),
			ann:  ann("distroless"),
			want: "gcr.io/gke-release/asm/proxyv2:1.11.2-asm.17-distroless",
		},
		{
			desc: "pc and annotation overrides imageType",
			v:    val("gcr.io/gke-release/asm", "1.11.2-asm.17"),
			pc:   pc("distroless"),
			ann:  ann("debug"),
			want: "gcr.io/gke-release/asm/proxyv2:1.11.2-asm.17-debug",
		},
		{
			desc: "pc and annotation overrides imageType, ann is default",
			v:    val("gcr.io/gke-release/asm", "1.11.2-asm.17"),
			pc:   pc("debug"),
			ann:  ann("default"),
			want: "gcr.io/gke-release/asm/proxyv2:1.11.2-asm.17",
		},
		{
			desc: "pc overrides imageType with default, tag also has image type",
			v:    val("gcr.io/gke-release/asm", "1.11.2-asm.17-distroless"),
			pc:   pc("default"),
			want: "gcr.io/gke-release/asm/proxyv2:1.11.2-asm.17",
		},
		{
			desc: "ann overrides imageType with default, tag also has image type",
			v:    val("gcr.io/gke-release/asm", "1.11.2-asm.17-distroless"),
			ann:  ann("default"),
			want: "gcr.io/gke-release/asm/proxyv2:1.11.2-asm.17",
		},
		{
			desc: "pc overrides imageType, tag also has image type",
			v:    val("docker.io/istio", "1.12-debug"),
			pc:   pc("distroless"),
			want: "docker.io/istio/proxyv2:1.12-distroless",
		},
		{
			desc: "annotation overrides imageType, tag also has the same image type",
			v:    val("docker.io/istio", "1.12-distroless"),
			ann:  ann("distroless"),
			want: "docker.io/istio/proxyv2:1.12-distroless",
		},
		{
			desc: "unusual tag should work",
			v:    val("private-repo/istio", "1.12-this-is-unusual-tag"),
			want: "private-repo/istio/proxyv2:1.12-this-is-unusual-tag",
		},
		{
			desc: "unusual tag should work, default override",
			v:    val("private-repo/istio", "1.12-this-is-unusual-tag-distroless"),
			pc:   pc("default"),
			want: "private-repo/istio/proxyv2:1.12-this-is-unusual-tag",
		},
		{
			desc: "annotation overrides imageType with unusual tag",
			v:    val("private-repo/istio", "1.12-this-is-unusual-tag"),
			ann:  ann("distroless"),
			want: "private-repo/istio/proxyv2:1.12-this-is-unusual-tag-distroless",
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			got := ProxyImage(tt.v, tt.pc, tt.ann)
			if got != tt.want {
				t.Errorf("got: <%s>, want <%s> <== value(%v) proxyConfig(%v) ann(%v)", got, tt.want, tt.v, tt.pc, tt.ann)
			}
		})
	}
}

func podWithEnv(envCount int) *corev1.Pod {
	envs := []corev1.EnvVar{}
	for i := 0; i < envCount; i++ {
		envs = append(envs, corev1.EnvVar{
			Name:  fmt.Sprintf("something-%d", i),
			Value: "blah",
		})
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "fake",
				Env:   envs,
			}},
		},
	}
}

// TestInjection tests both the mutating webhook and kube-inject. It does this by sharing the same input and output
// test files and running through the two different code paths.
func BenchmarkInjection(b *testing.B) {
	istiolog.FindScope("default").SetOutputLevel(istiolog.ErrorLevel)
	cases := []struct {
		name string
		in   *corev1.Pod
	}{
		{
			name: "many env vars",
			in:   podWithEnv(2000),
		},
	}

	for _, tt := range cases {
		b.Run(tt.name, func(b *testing.B) {
			// Preload default settings. Computation here is expensive, so this speeds the tests up substantially
			sidecarTemplate, valuesConfig, mc := getInjectionSettings(b, nil, "")
			env := &model.Environment{}
			env.SetPushContext(&model.PushContext{
				ProxyConfigs: &model.ProxyConfigs{},
			})
			webhook := &Webhook{
				Config:       sidecarTemplate,
				meshConfig:   mc,
				env:          env,
				valuesConfig: valuesConfig,
				revision:     "default",
			}
			templateJSON := convertToJSON(tt.in, b)
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				webhook.inject(&kube.AdmissionReview{
					Request: &kube.AdmissionRequest{
						Object: runtime.RawExtension{
							Raw: templateJSON,
						},
						Namespace: tt.in.Namespace,
					},
				}, "")
			}
		})
	}
}

func TestNativeSidecarAnnotationOverride(t *testing.T) {
	funcMap := template.FuncMap{
		"default": func(defaultVal, val string) string {
			if val == "" {
				return defaultVal
			}
			return val
		},
	}

	tmpl := template.Must(template.New("inject").Funcs(funcMap).Parse(`
{{- $nativeSidecar := ne (index .ObjectMeta.Annotations ` + "`" + `sidecar.istio.io/nativeSidecar` + "`" + ` | default (printf "%t" .NativeSidecars)) "false" }}
{{- if $nativeSidecar }}
spec:
  initContainers:
  - name: istio-proxy
    image: proxy
{{- else }}
spec:
  containers:
  - name: istio-proxy
    image: proxy
{{- end }}
`))

	cases := []struct {
		name            string
		annotations     map[string]string
		nativeSidecar   bool
		expectContainer string
	}{
		{
			name:            "global flag enabled, no annotation",
			annotations:     map[string]string{},
			nativeSidecar:   true,
			expectContainer: "initContainers",
		},
		{
			name: "global flag enabled, annotation disabled",
			annotations: map[string]string{
				"sidecar.istio.io/nativeSidecar": "false",
			},
			nativeSidecar:   true,
			expectContainer: "containers",
		},
		{
			name: "global flag enabled, annotation enabled",
			annotations: map[string]string{
				"sidecar.istio.io/nativeSidecar": "true",
			},
			nativeSidecar:   true,
			expectContainer: "initContainers",
		},
		{
			name: "global flag disabled, annotation enabled",
			annotations: map[string]string{
				"sidecar.istio.io/nativeSidecar": "true",
			},
			nativeSidecar:   false,
			expectContainer: "initContainers",
		},
		{
			name:            "global flag disabled, no annotation",
			annotations:     map[string]string{},
			nativeSidecar:   false,
			expectContainer: "containers",
		},
		{
			name: "global flag disabled, annotation disabled",
			annotations: map[string]string{
				"sidecar.istio.io/nativeSidecar": "false",
			},
			nativeSidecar:   false,
			expectContainer: "containers",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tc.annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "app",
						},
						{
							Name: "istio-proxy",
						},
					},
				},
			}
			_, valuesConfig, meshConfig := getInjectionSettings(t, nil, "")

			params := InjectionParameters{
				pod:             pod,
				templates:       Templates{SidecarTemplateName: tmpl},
				defaultTemplate: []string{SidecarTemplateName},
				meshConfig:      meshConfig,
				valuesConfig:    valuesConfig,
				nativeSidecar:   tc.nativeSidecar,
				proxyConfig:     mesh.DefaultProxyConfig(),
			}

			mergedPod, _, err := RunTemplate(params)
			if err != nil {
				t.Fatalf("RunTemplate() error = %v", err)
			}

			if tc.expectContainer == "initContainers" {
				if FindContainer(ProxyContainerName, mergedPod.Spec.InitContainers) == nil {
					t.Errorf("Expected to find istio-proxy in initContainers")
				}
				if FindContainer(ProxyContainerName, mergedPod.Spec.Containers) != nil {
					t.Errorf("Expected not to find istio-proxy in containers")
				}
			} else {
				if FindContainer(ProxyContainerName, mergedPod.Spec.Containers) == nil {
					t.Errorf("Expected to find istio-proxy in containers")
				}
				if FindContainer(ProxyContainerName, mergedPod.Spec.InitContainers) != nil {
					t.Errorf("Expected not to find istio-proxy in initContainers")
				}
			}
		})
	}
}
