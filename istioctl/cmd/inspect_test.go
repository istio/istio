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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
)

// execAndK8sConfigTestCase lets a test case hold some Envoy, Istio, and Kubernetes configuration
type execAndK8sConfigTestCase struct {
	execClientConfig map[string][]byte // Canned Envoy configuration
	configs          []model.Config    // Canned Istio configuration
	k8sConfigs       []runtime.Object  // Canned K8s configuration

	args []string

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain
	goldenFilename string // Expected output stored in golden file

	wantException bool
}

var (
	cannedIstioConfig = []model.Config{}

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
		}},
	}
)

func TestInspect(t *testing.T) {
	cannedConfig := map[string][]byte{
		"details-v1-5b7f94f9bc-wp5tb": util.ReadFile("../pkg/writer/compare/testdata/envoyconfigdump.json", t),
		"istio-pilot-7f9796fc98-99bp7": []byte(`[
{
    "host": "details.default.svc.cluster.local",
    "port": 9080,
    "authentication_policy_name": "default/",
    "destination_rule_name": "details/default",
    "server_protocol": "HTTP/mTLS",
    "client_protocol": "HTTP",
    "TLS_conflict_status": "OK"
}
]`),
	}
	cases := []execAndK8sConfigTestCase{
		{ // case 0
			args:           strings.Split("experimental inspect", " "),
			expectedString: "Inspect resources for Istio configuration",
		},
		{ // case 1 short name 'i'
			args:           strings.Split("x i", " "),
			expectedString: "Inspect resources for Istio configuration",
		},
		{ // case 2 no pod
			args:           strings.Split("experimental inspect pod", " "),
			expectedString: "Error: expecting pod name",
			wantException:  true, // "istioctl experimental inspect pod" should fail
		},
		{ // case 3 unknown pod
			args:           strings.Split("experimental inspect pod not-a-pod", " "),
			expectedString: "pods \"not-a-pod\" not found",
			wantException:  true, // "istioctl experimental inspect pod not-a-pod" should fail
		},
		{ // case 4 has data
			execClientConfig: cannedConfig,
			configs:          cannedIstioConfig,
			k8sConfigs:       cannedK8sEnv,
			args:             strings.Split("experimental inspect pod details-v1-5b7f94f9bc-wp5tb", " "),
			expectedOutput: `Pod: details-v1-5b7f94f9bc-wp5tb
   Pod Ports: 15090 (istio-proxy)
Suggestion: add 'version' label to pod for Istio telemetry.
--------------------
Service: details
Pilot reports that pod enforces HTTP/mTLS and clients speak HTTP
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
