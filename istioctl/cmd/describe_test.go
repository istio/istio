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

package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pilot/test/util"
)

// execAndK8sConfigTestCase lets a test case hold some Envoy, Istio, and Kubernetes configuration
type execAndK8sConfigTestCase struct {
	k8sConfigs []runtime.Object // Canned K8s configuration
	namespace  string

	args []string

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain
	goldenFilename string // Expected output stored in golden file

	wantException bool
}

// Tests Pilot /debug
func TestDescribe(t *testing.T) {
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
		{ // case 8 unknown service
			args:           strings.Split("experimental describe service not-a-service", " "),
			expectedString: "services \"not-a-service\" not found",
			wantException:  true, // "istioctl experimental describe service not-a-service" should fail
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

	// Override the Istio config factory
	configStoreFactory = mockClientFactoryGenerator()

	// Override the K8s config factory
	interfaceFactory = mockInterfaceFactoryGenerator(c.k8sConfigs)

	var out bytes.Buffer
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	if c.namespace != "" {
		namespace = c.namespace
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
