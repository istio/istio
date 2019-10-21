// Copyright 2019 Istio Authors
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

package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/schemas"
)

// execAndK8sConfigTestCase lets a test case hold some Envoy, Istio, and Kubernetes configuration
type execAndK8sConfigTestCase struct {
	execClientConfig map[string][]byte // Canned Envoy configuration
	configs          []model.Config    // Canned Istio configuration
	k8sConfigs       []runtime.Object  // Canned K8s configuration
	namespace        string

	args []string

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain
	goldenFilename string // Expected output stored in golden file

	wantException bool
}

var (
	cannedIngressGatewayService = coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "istio-ingressgateway",
			Namespace: "istio-system",
			Labels: map[string]string{
				"istio": "ingressgateway",
			},
		},
		Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{
				{
					Port:     80,
					NodePort: 31380,
					Name:     "http2",
					Protocol: "TCP",
				},
			},
			Selector: map[string]string{"istio": "ingressgateway"},
		},
		Status: coreV1.ServiceStatus{
			LoadBalancer: coreV1.LoadBalancerStatus{
				Ingress: []coreV1.LoadBalancerIngress{
					{
						IP: "10.1.2.3",
					},
				},
			},
		},
	}

	cannedIngressGatewayPod = coreV1.Pod{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "istio-ingressgateway-5bf6c9887-vvvmj",
			Namespace: "istio-system",
			Labels: map[string]string{
				"istio": "ingressgateway",
			},
		},
		Spec: coreV1.PodSpec{
			NodeName: "foo_node",
			Containers: []coreV1.Container{
				{
					Name: "istio-proxy",
				},
			},
		},
		Status: coreV1.PodStatus{
			Phase: coreV1.PodRunning,
		},
	}

	cannedIstioConfig = []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Name:      "ratings",
				Namespace: "bookinfo",
				Type:      schemas.DestinationRule.Type,
				Group:     schemas.DestinationRule.Group,
				Version:   schemas.DestinationRule.Version,
			},
			Spec: &networking.DestinationRule{
				Host: "ratings",
				Subsets: []*networking.Subset{
					{
						Name: "v1",
						Labels: map[string]string{
							"version": "v1",
						},
					},
				},
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.TLSSettings{
						Mode: networking.TLSSettings_ISTIO_MUTUAL,
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{
				Name:      "bookinfo",
				Namespace: "default",
				Type:      schemas.VirtualService.Type,
				Group:     schemas.VirtualService.Group,
				Version:   schemas.VirtualService.Version,
			},
			Spec: &networking.VirtualService{
				Hosts:    []string{"*"},
				Gateways: []string{"bookinfo-gateway"},
				Http: []*networking.HTTPRoute{
					{
						Match: []*networking.HTTPMatchRequest{
							{
								Uri: &networking.StringMatch{
									MatchType: &networking.StringMatch_Exact{Exact: "/productpage"},
								},
							},
							{
								Uri: &networking.StringMatch{
									MatchType: &networking.StringMatch_Exact{Exact: "/login"},
								},
							},
							{
								Uri: &networking.StringMatch{
									MatchType: &networking.StringMatch_Exact{Exact: "/logout"},
								},
							},
							{
								Uri: &networking.StringMatch{
									MatchType: &networking.StringMatch_Prefix{Prefix: "/api/v1/products"},
								},
							},
						},
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "productpage",
									Port: &networking.PortSelector{
										Number: 80,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	cannedK8sEnv = []runtime.Object{
		&coreV1.PodList{Items: []coreV1.Pod{
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "details-v1-5b7f94f9bc-wp5tb",
					Namespace: "default",
					Labels: map[string]string{
						"app": "details",
					},
				},
				Spec: coreV1.PodSpec{
					NodeName: "foo_node",
					Containers: []coreV1.Container{
						{
							Name: "istio-proxy",
							Ports: []coreV1.ContainerPort{
								{
									Name:          "http-envoy-prom",
									ContainerPort: 15090,
									Protocol:      "TCP",
								},
							},
						},
					},
				},
				Status: coreV1.PodStatus{
					Phase: coreV1.PodRunning,
				},
			},
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "ratings-v1-f745cf57b-vfwcv",
					Namespace: "bookinfo",
					Labels: map[string]string{
						"app":     "ratings",
						"version": "v1",
					},
				},
				Spec: coreV1.PodSpec{
					NodeName: "foo_node",
					Containers: []coreV1.Container{
						{
							Name: "ratings",
							Ports: []coreV1.ContainerPort{
								{
									ContainerPort: 9080,
									Protocol:      "TCP",
								},
							},
						},
						{
							Name: "istio-proxy",
							Ports: []coreV1.ContainerPort{
								{
									Name:          "http-envoy-prom",
									ContainerPort: 15090,
									Protocol:      "TCP",
								},
							},
						},
					},
				},
				Status: coreV1.PodStatus{
					Phase: coreV1.PodRunning,
					ContainerStatuses: []coreV1.ContainerStatus{
						{
							Name:  "istio-proxy",
							Ready: true,
						},
					},
				},
			},
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "productpage-v1-7bbd79f8fd-k6j79",
					Namespace: "default",
					Labels: map[string]string{
						"app":     "productpage",
						"version": "v1",
					},
				},
				Spec: coreV1.PodSpec{
					NodeName: "foo_node",
					Containers: []coreV1.Container{
						{
							Name: "productpage",
							// No container port, but the Envoy data will show 1.3 Istio
						},
						{
							Name: "istio-proxy",
							Ports: []coreV1.ContainerPort{
								{
									Name:          "http-envoy-prom",
									ContainerPort: 15090,
									Protocol:      "TCP",
								},
							},
						},
					},
				},
				Status: coreV1.PodStatus{
					Phase: coreV1.PodRunning,
				},
			},
			cannedIngressGatewayPod,
		}},
		&coreV1.ServiceList{Items: []coreV1.Service{
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "details",
					Namespace: "default",
				},
				Spec: coreV1.ServiceSpec{
					Ports: []coreV1.ServicePort{
						{
							Port: 9080,
							Name: "http",
						},
					},
					Selector: map[string]string{"app": "details"},
				},
			},
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "ratings",
					Namespace: "bookinfo",
				},
				Spec: coreV1.ServiceSpec{
					Ports: []coreV1.ServicePort{
						{
							Port: 9080,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 9080,
							},
							Name:     "http",
							Protocol: "TCP",
						},
					},
					Selector: map[string]string{"app": "ratings"},
				},
			},
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "productpage",
					Namespace: "default",
				},
				Spec: coreV1.ServiceSpec{
					Ports: []coreV1.ServicePort{
						{
							Port: 9080,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 9080,
							},
							Protocol: "TCP",
						},
					},
					Selector: map[string]string{"app": "productpage"},
				},
			},
			cannedIngressGatewayService,
		}},
	}

	cannedNoPortNameK8sEnv = []runtime.Object{
		&coreV1.PodList{Items: []coreV1.Pod{
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "ratings-v1-f745cf57b-vfwcv",
					Namespace: "bookinfo",
					Labels: map[string]string{
						"app":     "ratings",
						"version": "v1",
					},
				},
				Spec: coreV1.PodSpec{
					NodeName: "foo_node",
					Containers: []coreV1.Container{
						{
							Name: "ratings",
						},
						{
							Name: "istio-proxy",
							Ports: []coreV1.ContainerPort{
								{
									Name:          "http-envoy-prom",
									ContainerPort: 15090,
									Protocol:      "TCP",
								},
							},
						},
					},
				},
				Status: coreV1.PodStatus{
					Phase: coreV1.PodRunning,
				},
			},
			cannedIngressGatewayPod,
		}},
		&coreV1.ServiceList{Items: []coreV1.Service{
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "ratings",
					Namespace: "bookinfo",
				},
				Spec: coreV1.ServiceSpec{
					Ports: []coreV1.ServicePort{
						{
							Port: 9080,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 9080,
							},
							Protocol: "TCP",
						},
					},
					Selector: map[string]string{"app": "ratings"},
				},
			},
			cannedIngressGatewayService,
		}},
	}
)

func TestDescribe(t *testing.T) {
	cannedConfig := map[string][]byte{
		"details-v1-5b7f94f9bc-wp5tb":     util.ReadFile("../pkg/writer/compare/testdata/envoyconfigdump.json", t),
		"ratings-v1-f745cf57b-vfwcv":      util.ReadFile("testdata/describe/ratings-v1-f745cf57b-vfwcv.json", t),
		"productpage-v1-7bbd79f8fd-k6j79": util.ReadFile("testdata/describe/productpage-v1-7bbd79f8fd-k6j79.json", t),
		"istio-pilot-7f9796fc98-99bp7": []byte(`[
{
    "host": "details.default.svc.cluster.local",
    "port": 9080,
    "authentication_policy_name": "default/",
    "destination_rule_name": "details/default",
    "server_protocol": "HTTP/mTLS",
    "client_protocol": "HTTP",
    "TLS_conflict_status": "OK"
},
{
    "host": "ratings.bookinfo.svc.cluster.local",
    "port": 9080,
    "authentication_policy_name": "default/",
    "destination_rule_name": "details/default",
    "server_protocol": "HTTP/mTLS",
    "client_protocol": "mTLS",
    "TLS_conflict_status": "OK"
}
]`),
		"istio-ingressgateway-5bf6c9887-vvvmj": util.ReadFile("testdata/describe/istio-ingressgateway-5bf6c9887-vvvmj.json", t),
	}
	cases := []execAndK8sConfigTestCase{
		{ // case 0
			args:           strings.Split("experimental describe", " "),
			expectedString: "Describe resource and related Istio configuration",
		},
		{ // case 1 short name 'i'
			args:           strings.Split("x des", " "),
			expectedString: "Describe resource and related Istio configuration",
		},
		{ // case 2 no pod
			args:           strings.Split("experimental describe pod", " "),
			expectedString: "Error: expecting pod name",
			wantException:  true, // "istioctl experimental inspect pod" should fail
		},
		{ // case 3 unknown pod
			args:           strings.Split("experimental describe pod not-a-pod", " "),
			expectedString: "pods \"not-a-pod\" not found",
			wantException:  true, // "istioctl experimental describe pod not-a-pod" should fail
		},
		{ // case 4 has data
			execClientConfig: cannedConfig,
			configs:          cannedIstioConfig,
			k8sConfigs:       cannedK8sEnv,
			namespace:        "default",
			args:             strings.Split("experimental describe pod details-v1-5b7f94f9bc-wp5tb", " "),
			expectedOutput: `Pod: details-v1-5b7f94f9bc-wp5tb
   Pod Ports: 15090 (istio-proxy)
Suggestion: add 'version' label to pod for Istio telemetry.
--------------------
Service: details
Pilot reports that pod is PERMISSIVE (enforces HTTP/mTLS) and clients speak HTTP
`,
		},
		{ // case 5 has recent data including RBAC
			execClientConfig: cannedConfig,
			configs:          cannedIstioConfig,
			k8sConfigs:       cannedK8sEnv,
			args:             strings.Split("-n bookinfo experimental describe pod ratings-v1-f745cf57b-vfwcv", " "),
			expectedOutput: `Pod: ratings-v1-f745cf57b-vfwcv
   Pod Ports: 9080 (ratings), 15090 (istio-proxy)
--------------------
Service: ratings
   Port: http 9080/HTTP targets pod port 9080
DestinationRule: ratings for "ratings"
   Matching subsets: v1
   Traffic Policy TLS Mode: ISTIO_MUTUAL
Pilot reports that pod is PERMISSIVE (enforces HTTP/mTLS) and clients speak mTLS
RBAC policies: ratings-reader
`,
		},
		{ // case 6 has 1.3 data, and a service with unnamed port
			execClientConfig: cannedConfig,
			configs:          cannedIstioConfig,
			k8sConfigs:       cannedK8sEnv,
			args:             strings.Split("-n default experimental describe pod productpage-v1-7bbd79f8fd-k6j79", " "),
			expectedOutput: `Pod: productpage-v1-7bbd79f8fd-k6j79
   Pod Ports: 15090 (istio-proxy)
--------------------
Service: productpage
   Port:  9080/UnsupportedProtocol targets pod port 9080
   9080 is unnamed which does not follow Istio conventions
Authn: None


Exposed on Ingress Gateway http://10.1.2.3:0

VirtualService: bookinfo
   /productpage, /login, /logout, /api/v1/products*
`,
		},
		{ // case 7 has 1.2 data, and a service with unnamed port, and no containerPort
			execClientConfig: cannedConfig,
			configs:          []model.Config{},
			k8sConfigs:       cannedNoPortNameK8sEnv,
			args:             strings.Split("-n bookinfo experimental describe pod ratings-v1-f745cf57b-vfwcv", " "),
			expectedOutput: `Pod: ratings-v1-f745cf57b-vfwcv
   Pod Ports: 15090 (istio-proxy)
--------------------
Service: ratings
   Port:  9080/UnsupportedProtocol targets pod port 9080
   Warning: Pod ratings-v1-f745cf57b-vfwcv port 9080 not exposed by Container
   9080 is unnamed which does not follow Istio conventions
Pilot reports that pod is PERMISSIVE (enforces HTTP/mTLS) and clients speak mTLS
RBAC policies: ratings-reader
`,
		},
		{ // case 8 unknown service
			args:           strings.Split("experimental describe service not-a-service", " "),
			expectedString: "services \"not-a-service\" not found",
			wantException:  true, // "istioctl experimental describe service not-a-service" should fail
		},
		{ // case 9 for a service
			execClientConfig: cannedConfig,
			configs:          cannedIstioConfig,
			k8sConfigs:       cannedK8sEnv,
			args:             strings.Split("x describe svc ratings.bookinfo", " "),
			expectedOutput: `Service: ratings.bookinfo
   Port: http 9080/HTTP targets pod port 9080
DestinationRule: ratings.bookinfo for "ratings"
   Matching subsets: v1
   Traffic Policy TLS Mode: ISTIO_MUTUAL
Pilot reports that pod is PERMISSIVE (enforces HTTP/mTLS) and clients speak mTLS
RBAC policies: ratings-reader
`,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyExecAndK8sConfigTestCaseTestOutput(t, c)
		})
	}
}

func verifyExecAndK8sConfigTestCaseTestOutput(t *testing.T, c execAndK8sConfigTestCase) {
	t.Helper()

	// Override the exec client factory
	clientExecFactory = mockClientExecFactoryGenerator(c.execClientConfig)

	// Override the Istio config factory
	clientFactory = mockClientFactoryGenerator(c.configs)

	// Override the K8s config factory
	interfaceFactory = mockInterfaceFactoryGenerator(c.k8sConfigs)

	var out bytes.Buffer
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOutput(&out)
	if c.namespace != "" {
		namespace = c.namespace
	}

	file = "" // Clear, because we re-use

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v", strings.Join(c.args, " "), output, c.expectedString)
	}

	if c.goldenFilename != "" {
		util.CompareContent([]byte(output), c.goldenFilename, t)
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

func mockInterfaceFactoryGenerator(k8sConfigs []runtime.Object) func(kubeconfig string) (kubernetes.Interface, error) {
	outFactory := func(_ string) (kubernetes.Interface, error) {
		client := fake.NewSimpleClientset(k8sConfigs...)
		return client, nil
	}

	return outFactory
}
