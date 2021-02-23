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
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
)

// TestInjection tests both the mutating webhook and kube-inject. It does this by sharing the same input and output
// test files and running through the two different code paths.
func TestInjection(t *testing.T) {
	type testCase struct {
		in            string
		want          string
		setFlags      []string
		inFilePath    string
		mesh          func(m *meshapi.MeshConfig)
		skipWebhook   bool
		expectedError string
		setup         func()
		teardown      func()
	}
	cases := []testCase{
		// verify cni
		{
			in:   "hello.yaml",
			want: "hello.yaml.cni.injected",
			setFlags: []string{
				"components.cni.enabled=true",
				"values.istio_cni.chained=true",
				"values.global.network=network1",
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
			in:       "enable-core-dump.yaml",
			want:     "enable-core-dump.yaml.injected",
			setFlags: []string{"values.global.proxy.enableCoreDump=true"},
		},
		{
			in:   "format-duration.yaml",
			want: "format-duration.yaml.injected",
			mesh: func(m *meshapi.MeshConfig) {
				m.DefaultConfig.DrainDuration = types.DurationProto(time.Second * 23)
				m.DefaultConfig.ParentShutdownDuration = types.DurationProto(time.Second * 42)
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
				`values.istio_cni.chained=false`,
			},
		},
		{
			// Verifies that istio-cni is appended to k8s.v1.cni.cncf.io/networks flat value if set
			in:   "hello-existing-cncf-networks.yaml",
			want: "hello-existing-cncf-networks.yaml.injected",
			setFlags: []string{
				`components.cni.enabled=true`,
				`values.istio_cni.chained=false`,
			},
		},
		{
			// Verifies that istio-cni is appended to k8s.v1.cni.cncf.io/networks JSON value
			in:   "hello-existing-cncf-networks-json.yaml",
			want: "hello-existing-cncf-networks-json.yaml.injected",
			setFlags: []string{
				`components.cni.enabled=true`,
				`values.istio_cni.chained=false`,
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
			in:   "hello.yaml",
			want: "hello-no-seccontext.yaml.injected",
			setup: func() {
				features.EnableLegacyFSGroupInjection = false
				os.Setenv("ENABLE_LEGACY_FSGROUP_INJECTION", "false")
			},
			teardown: func() {
				features.EnableLegacyFSGroupInjection = true
				os.Setenv("ENABLE_LEGACY_FSGROUP_INJECTION", "true")
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
			in:         "custom-template.yaml",
			want:       "custom-template.yaml.injected",
			inFilePath: "custom-template.iop.yaml",
		},
	}
	// Keep track of tests we add options above
	// We will search for all test files and skip these ones
	alreadyTested := sets.NewSet()
	for _, t := range cases {
		if t.want != "" {
			alreadyTested.Insert(t.want)
		} else {
			alreadyTested.Insert(t.in + ".injected")
		}
	}
	files, err := ioutil.ReadDir("testdata/inject")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 {
		t.Fatalf("Didn't find test files - something must have gone wrong")
	}
	// Automatically add any other test files in the folder. This ensures we don't
	// forget to add to this list, that we don't have duplicates, etc
	// Keep track of all golden files so we can ensure we don't have unused ones later
	allOutputFiles := sets.NewSet()
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
	defaultTemplate, defaultValues, defaultMesh := loadInjectionSettings(t, nil, "")
	for i, c := range cases {
		c := c
		testName := fmt.Sprintf("[%02d] %s", i, c.want)
		if c.expectedError != "" {
			testName = fmt.Sprintf("[%02d] %s", i, c.in)
		}
		t.Run(testName, func(t *testing.T) {
			if c.setup != nil {
				c.setup()
			} else {
				// Tests with custom setup modify global state and cannot run in parallel
				t.Parallel()
			}
			if c.teardown != nil {
				t.Cleanup(c.teardown)
			}

			mc, err := mesh.DeepCopyMeshConfig(defaultMesh)
			if err != nil {
				t.Fatal(err)
			}
			sidecarTemplate, valuesConfig := defaultTemplate, defaultValues
			if c.setFlags != nil || c.inFilePath != "" {
				sidecarTemplate, valuesConfig, mc = loadInjectionSettings(t, c.setFlags, c.inFilePath)
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
				var got bytes.Buffer
				warn := func(s string) {
					t.Log(s)
				}
				if err = IntoResourceFile(sidecarTemplate.Templates, valuesConfig, "", mc, in, &got, warn); err != nil {
					if c.expectedError != "" {
						if !strings.Contains(strings.ToLower(err.Error()), c.expectedError) {
							t.Fatalf("expected error %q got %q", c.expectedError, err)
						}
						return
					}
					t.Fatalf("IntoResourceFile(%v) returned an error: %v", inputFilePath, err)
				}
				if c.expectedError != "" {
					t.Fatalf("expected error but got none")
				}

				// The version string is a maintenance pain for this test. Strip the version string before comparing.
				gotBytes := util.StripVersion(got.Bytes())
				wantBytes := util.ReadGoldenFile(gotBytes, wantFilePath, t)

				util.CompareBytes(gotBytes, wantBytes, wantFilePath, t)
			})

			// Exit early if we don't need to test webhook. We can skip errors since its redundant
			// and painful to test here.
			if c.expectedError != "" || c.skipWebhook {
				return
			}
			// Next run the webhook test. This one is a bit trickier as the webhook operates
			// on Pods, but the inputs are Deployments/StatefulSets/etc. As a result, we need
			// to convert these to pods, then run the injection This test will *not*
			// overwrite golden files, as we do not have identical textual output as
			// kube-inject. Instead, we just compare the desired/actual pod specs.
			t.Run("webhook", func(t *testing.T) {
				webhook := &Webhook{
					Config:       sidecarTemplate,
					meshConfig:   mc,
					valuesConfig: valuesConfig,
					revision:     "default",
				}
				// Split multi-part yaml documents. Input and output will have the same number of parts.
				inputYAMLs := splitYamlFile(inputFilePath, t)
				wantYAMLs := splitYamlFile(wantFilePath, t)
				for i := 0; i < len(inputYAMLs); i++ {
					t.Run(fmt.Sprintf("yamlPart[%d]", i), func(t *testing.T) {
						runWebhook(t, webhook, inputYAMLs[i], wantYAMLs[i], false)
						t.Run("idempotency", func(t *testing.T) {
							runWebhook(t, webhook, wantYAMLs[i], wantYAMLs[i], true)
						})
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

func testInjectionTemplate(t *testing.T, template, input, expected string) {
	t.Helper()
	webhook := &Webhook{
		Config: &Config{
			Templates:        map[string]string{SidecarTemplateName: template},
			Policy:           InjectionPolicyEnabled,
			DefaultTemplates: []string{SidecarTemplateName},
		},
	}
	runWebhook(t, webhook, []byte(input), []byte(expected), false)
}

func TestMultipleInjectionTemplates(t *testing.T) {
	webhook := &Webhook{
		Config: &Config{
			Templates: map[string]string{
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
			},
			Aliases: map[string][]string{"both": {"sidecar", "init"}},
			Policy:  InjectionPolicyEnabled,
		},
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

func runWebhook(t *testing.T, webhook *Webhook, inputYAML []byte, wantYAML []byte, ignoreIstioMetaJSONAnnotationsEnv bool) {
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

	if err := normalizeAndCompareDeployments(gotPod, wantPod, ignoreIstioMetaJSONAnnotationsEnv, t); err != nil {
		t.Fatal(err)
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
			t.Fatalf("unexpect ports result for case %d", i)
		}
		for j := 0; j < len(ports); j++ {
			if ports[j] != expectPorts[j] {
				t.Fatalf("unexpect ports result for case %d: expect %v, got %v", i, expectPorts, ports)
			}
		}
	}
}

func TestCleanProxyConfig(t *testing.T) {
	overrides := mesh.DefaultProxyConfig()
	overrides.ConfigPath = "/foo/bar"
	overrides.DrainDuration = types.DurationProto(7 * time.Second)
	overrides.ProxyMetadata = map[string]string{
		"foo": "barr",
	}
	explicit := mesh.DefaultProxyConfig()
	explicit.ConfigPath = constants.ConfigPathDir
	explicit.DrainDuration = types.DurationProto(45 * time.Second)
	cases := []struct {
		name   string
		proxy  meshapi.ProxyConfig
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
			got := protoToJSON(&tt.proxy)
			if got != tt.expect {
				t.Fatalf("incorrect output: got %v, expected %v", got, tt.expect)
			}
			roundTrip, err := mesh.ApplyProxyConfig(got, mesh.DefaultMeshConfig())
			if err != nil {
				t.Fatal(err)
			}
			if !cmp.Equal(*roundTrip.GetDefaultConfig(), tt.proxy) {
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
