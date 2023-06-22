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

package describe

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiannotation "istio.io/api/annotation"
	v1alpha32 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/util/assert"
)

// execAndK8sConfigTestCase lets a test case hold some Envoy, Istio, and Kubernetes configuration
type execAndK8sConfigTestCase struct {
	k8sConfigs     []runtime.Object // Canned K8s configuration
	istioConfigs   []runtime.Object // Canned Istio configuration
	configDumps    map[string][]byte
	namespace      string
	istioNamespace string

	args []string

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain
	goldenFilename string // Expected output stored in golden file

	wantException bool
}

// Tests Pilot /debug
func TestDescribe(t *testing.T) {
	productPageConfigPath := "testdata/describe/http_config.json"
	config, err := os.ReadFile(productPageConfigPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", productPageConfigPath, err)
	}
	cases := []execAndK8sConfigTestCase{
		{ // case 0
			args:           []string{},
			expectedString: "Describe resource and related Istio configuration",
		},
		{ // case 2 no pod
			args:           strings.Split("pod", " "),
			expectedString: "Error: expecting pod name",
			wantException:  true, // "istioctl experimental inspect pod" should fail
		},
		{ // case 3 unknown pod
			args:           strings.Split("po not-a-pod", " "),
			expectedString: "pods \"not-a-pod\" not found",
			wantException:  true, // "istioctl experimental describe pod not-a-pod" should fail
		},
		{ // case 8 unknown service
			args:           strings.Split("service not-a-service", " "),
			expectedString: "services \"not-a-service\" not found",
			wantException:  true, // "istioctl experimental describe service not-a-service" should fail
		},
		{
			k8sConfigs: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "productpage",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							"app": "productpage",
						},
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       9080,
								TargetPort: intstr.FromInt(9080),
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ingress",
						Namespace: "default",
						Labels: map[string]string{
							"istio": "ingressgateway",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							"istio": "ingressgateway",
						},
						Ports: []corev1.ServicePort{
							{
								Name:       "http",
								Port:       80,
								TargetPort: intstr.FromInt(80),
							},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "productpage-v1-1234567890",
						Namespace: "default",
						Labels: map[string]string{
							"app": "productpage",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "productpage",
								Ports: []corev1.ContainerPort{
									{
										Name:          "http",
										ContainerPort: 9080,
									},
								},
							},
							{
								Name: "istio-proxy",
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "istio-proxy",
								Ready: true,
							},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ingress",
						Namespace: "default",
						Labels: map[string]string{
							"istio": "ingressgateway",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "istio-proxy",
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "istio-proxy",
								Ready: true,
							},
						},
					},
				},
			},
			istioConfigs: []runtime.Object{
				&v1alpha3.VirtualService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bookinfo",
						Namespace: "default",
					},
					Spec: v1alpha32.VirtualService{
						Hosts:    []string{"productpage"},
						Gateways: []string{"fake-gw"},
						Http: []*v1alpha32.HTTPRoute{
							{
								Match: []*v1alpha32.HTTPMatchRequest{
									{
										Uri: &v1alpha32.StringMatch{
											MatchType: &v1alpha32.StringMatch_Prefix{
												Prefix: "/prefix",
											},
										},
									},
								},
								Route: []*v1alpha32.HTTPRouteDestination{
									{
										Destination: &v1alpha32.Destination{
											Host: "productpage",
										},
										Weight: 30,
									},
									{
										Destination: &v1alpha32.Destination{
											Host: "productpage2",
										},
										Weight: 20,
									},
									{
										Destination: &v1alpha32.Destination{
											Host: "productpage3",
										},
										Weight: 50,
									},
								},
							},
						},
					},
				},
				&v1alpha3.DestinationRule{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "productpage",
						Namespace: "default",
					},
					Spec: v1alpha32.DestinationRule{
						Host: "productpage",
						Subsets: []*v1alpha32.Subset{
							{
								Name:   "v1",
								Labels: map[string]string{"version": "v1"},
							},
						},
					},
				},
			},
			configDumps: map[string][]byte{
				"productpage-v1-1234567890": config,
				"ingress":                   []byte("{}"),
			},
			namespace:      "default",
			istioNamespace: "default",
			// case 9, vs route to multiple hosts
			args: strings.Split("service productpage", " "),
			expectedOutput: `Service: productpage
DestinationRule: productpage for "productpage"
  WARNING POD DOES NOT MATCH ANY SUBSETS.  (Non matching subsets v1)
   Matching subsets: 
      (Non-matching subsets v1)
   No Traffic Policy
VirtualService: bookinfo
   Route to host "productpage" with weight 30%
   Route to host "productpage2" with weight 20%
   Route to host "productpage3" with weight 50%
   Match: /prefix*
`,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyExecAndK8sConfigTestCaseTestOutput(t, c)
		})
	}
}

func TestGetRevisionFromPodAnnotation(t *testing.T) {
	cases := []struct {
		anno klabels.Set

		expected string
	}{
		{
			anno: klabels.Set{
				apiannotation.SidecarStatus.Name: "",
			},
			expected: "",
		},
		{
			anno:     klabels.Set{},
			expected: "",
		},
		{
			anno: klabels.Set{
				apiannotation.SidecarStatus.Name: `
				{
					"initContainers": [
						"istio-init"
					],
					"containers": [
						"istio-proxy"
					],
					"volumes": [
						"istio-envoy",
						"istio-data",
						"istio-podinfo",
						"istio-token",
						"istiod-ca-cert"
					],
					"imagePullSecrets": null,
					"revision": "1-13-2"
				}`,
			},
			expected: "1-13-2",
		},
	}

	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			got := getRevisionFromPodAnnotation(tc.anno)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestFindProtocolForPort(t *testing.T) {
	http := "HTTP"
	cases := []struct {
		port             corev1.ServicePort
		expectedProtocol string
	}{
		{
			port: corev1.ServicePort{
				Name:     "http",
				Protocol: corev1.ProtocolTCP,
			},
			expectedProtocol: "HTTP",
		},
		{
			port: corev1.ServicePort{
				Name:     "GRPC-port",
				Protocol: corev1.ProtocolTCP,
			},
			expectedProtocol: "GRPC",
		},
		{
			port: corev1.ServicePort{
				AppProtocol: &http,
				Protocol:    corev1.ProtocolTCP,
			},
			expectedProtocol: "HTTP",
		},
		{
			port: corev1.ServicePort{
				Protocol: corev1.ProtocolTCP,
				Port:     80,
			},
			expectedProtocol: "auto-detect",
		},
		{
			port: corev1.ServicePort{
				Protocol: corev1.ProtocolUDP,
				Port:     80,
			},
			expectedProtocol: "UDP",
		},
	}

	for _, tc := range cases {
		protocol := findProtocolForPort(&tc.port)
		if protocol != tc.expectedProtocol {
			t.Fatalf("Output didn't match for the port protocol: got %s want %s", protocol, tc.expectedProtocol)
		}
	}
}

func verifyExecAndK8sConfigTestCaseTestOutput(t *testing.T, c execAndK8sConfigTestCase) {
	t.Helper()

	ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
		Namespace:      c.namespace,
		IstioNamespace: c.istioNamespace,
		Results:        c.configDumps,
	})
	client, err := ctx.CLIClient()
	assert.NoError(t, err)
	// Override the Istio config factory
	for i := range c.istioConfigs {
		switch t := c.istioConfigs[i].(type) {
		case *v1alpha3.DestinationRule:
			client.Istio().NetworkingV1alpha3().DestinationRules(c.namespace).Create(context.TODO(), t, metav1.CreateOptions{})
		case *v1alpha3.Gateway:
			client.Istio().NetworkingV1alpha3().Gateways(c.namespace).Create(context.TODO(), t, metav1.CreateOptions{})
		case *v1alpha3.VirtualService:
			client.Istio().NetworkingV1alpha3().VirtualServices(c.namespace).Create(context.TODO(), t, metav1.CreateOptions{})
		}
	}
	for i := range c.k8sConfigs {
		switch t := c.k8sConfigs[i].(type) {
		case *corev1.Service:
			client.Kube().CoreV1().Services(c.namespace).Create(context.TODO(), t, metav1.CreateOptions{})
		case *corev1.Pod:
			client.Kube().CoreV1().Pods(c.namespace).Create(context.TODO(), t, metav1.CreateOptions{})
		}
	}

	if c.configDumps == nil {
		c.configDumps = map[string][]byte{}
	}

	var out bytes.Buffer
	rootCmd := Cmd(ctx)
	rootCmd.SetArgs(c.args)

	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	if c.namespace != "" {
		describeNamespace = c.namespace
	}

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v", strings.Join(c.args, " "), output, c.expectedString)
	}

	if c.goldenFilename != "" {
		util.CompareContent(t, []byte(output), c.goldenFilename)
	}

	if c.wantException {
		if fErr == nil {
			t.Fatalf("Wanted an exception for 'istioctl %s', didn't get one, output was %q",
				strings.Join(c.args, " "), output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(c.args, " "), fErr)
		}
	}
}

func TestGetIstioVirtualServicePathForSvcFromRoute(t *testing.T) {
	tests := []struct {
		name         string
		inputConfig  string
		inputService corev1.Service
		inputPort    int32
		expected     string
	}{
		{
			name:        "test tls config",
			inputConfig: "testdata/describe/tls_config.json",
			inputService: corev1.Service{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "productpage",
					Namespace: "default",
				},
				Spec:   corev1.ServiceSpec{},
				Status: corev1.ServiceStatus{},
			},
			inputPort: int32(9080),
			expected:  "/apis/networking.istio.io/v1alpha3/namespaces/default/virtual-service/bookinfo",
		},
		{
			name:        "test http config",
			inputConfig: "testdata/describe/http_config.json",
			inputService: corev1.Service{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "productpage",
					Namespace: "default",
				},
				Spec:   corev1.ServiceSpec{},
				Status: corev1.ServiceStatus{},
			},
			inputPort: int32(9080),
			expected:  "/apis/networking.istio.io/v1alpha3/namespaces/default/virtual-service/bookinfo",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ic := util.ReadFile(t, test.inputConfig)
			cd := configdump.Wrapper{}
			err := cd.UnmarshalJSON(ic)
			if err != nil {
				t.Fatal(err)
			}
			out, err := getIstioVirtualServicePathForSvcFromRoute(&cd, test.inputService, test.inputPort)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(test.expected, out); diff != "" {
				t.Fatalf("Diff:\n%s", diff)
			}
		})
	}
}
