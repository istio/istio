// Copyright Istio Authors.
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
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"

	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/url"
)

type testcase struct {
	description       string
	expectedException bool
	args              []string
	k8sConfigs        []runtime.Object
	dynamicConfigs    []runtime.Object
	expectedOutput    string
	namespace         string
}

var (
	one              = int32(1)
	cannedK8sConfigs = []runtime.Object{
		&coreV1.ConfigMapList{Items: []coreV1.ConfigMap{}},

		&appsv1.DeploymentList{Items: []appsv1.Deployment{
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "details-v1",
					Namespace: "default",
					Labels: map[string]string{
						"app": "details",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &one,
					Selector: &metaV1.LabelSelector{
						MatchLabels: map[string]string{"app": "details"},
					},
					Template: coreV1.PodTemplateSpec{
						ObjectMeta: metaV1.ObjectMeta{
							Labels: map[string]string{"app": "details"},
						},
						Spec: coreV1.PodSpec{
							Containers: []coreV1.Container{
								{Name: "details", Image: "docker.io/istio/examples-bookinfo-details-v1:1.15.0"},
							},
						},
					},
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
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "dummyservice",
					Namespace: "default",
				},
				Spec: coreV1.ServiceSpec{
					Ports: []coreV1.ServicePort{
						{
							Port: 9080,
							Name: "http",
						},
					},
					Selector: map[string]string{"app": "dummy"},
				},
			},
		}},
	}
	cannedDynamicConfigs = []runtime.Object{
		&unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": collections.IstioNetworkingV1Alpha3Serviceentries.Resource().APIVersion(),
				"kind":       collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Kind(),
				"metadata": map[string]any{
					"namespace": "default",
					"name":      "mesh-expansion-vmtest",
				},
			},
		},
	}
)

func TestAddToMesh(t *testing.T) {
	cases := []testcase{
		{
			description:       "Invalid command args - missing service name",
			args:              strings.Split("experimental add-to-mesh service", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting service name\n",
		},
		{
			description:       "Invalid command args - missing deployment name",
			args:              strings.Split("experimental add-to-mesh deployment", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting deployment name\n",
		},
		{
			description: "valid case - add service into mesh",
			args: strings.Split("experimental add-to-mesh service details --meshConfigFile testdata/mesh-config.yaml"+
				" --injectConfigFile testdata/inject-config.yaml"+
				" --valuesFile testdata/inject-values.yaml", " "),
			expectedException: false,
			k8sConfigs:        cannedK8sConfigs,
			expectedOutput: "deployment details-v1.default updated successfully with Istio sidecar injected.\n" +
				"Next Step: Add related labels to the deployment to align with Istio's requirement: " + url.DeploymentRequirements + "\n",
			namespace: "default",
		},
		{
			description: "valid case - add deployment into mesh",
			args: strings.Split("experimental add-to-mesh deployment details-v1 --meshConfigFile testdata/mesh-config.yaml"+
				" --injectConfigFile testdata/inject-config.yaml"+
				" --valuesFile testdata/inject-values.yaml", " "),
			expectedException: false,
			k8sConfigs:        cannedK8sConfigs,
			expectedOutput: "deployment details-v1.default updated successfully with Istio sidecar injected.\n" +
				"Next Step: Add related labels to the deployment to align with Istio's requirement: " + url.DeploymentRequirements + "\n",
			namespace: "default",
		},
		{
			description: "service does not exist",
			args: strings.Split("experimental add-to-mesh service test --meshConfigFile testdata/mesh-config.yaml"+
				" --injectConfigFile testdata/inject-config.yaml"+
				" --valuesFile testdata/inject-values.yaml", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfigs,
			expectedOutput:    "Error: services \"test\" not found\n",
		},
		{
			description: "deployment does not exist",
			args: strings.Split("experimental add-to-mesh deployment test --meshConfigFile testdata/mesh-config.yaml"+
				" --injectConfigFile testdata/inject-config.yaml"+
				" --valuesFile testdata/inject-values.yaml", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfigs,
			expectedOutput:    "Error: deployment \"test\" does not exist\n",
		},
		{
			description: "service does not exist (with short syntax)",
			args: strings.Split("x add svc test --meshConfigFile testdata/mesh-config.yaml"+
				" --injectConfigFile testdata/inject-config.yaml"+
				" --valuesFile testdata/inject-values.yaml", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfigs,
			expectedOutput:    "Error: services \"test\" not found\n",
		},
		{
			description: "deployment does not exist (with short syntax)",
			args: strings.Split("x add deploy test --meshConfigFile testdata/mesh-config.yaml"+
				" --injectConfigFile testdata/inject-config.yaml"+
				" --valuesFile testdata/inject-values.yaml", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfigs,
			expectedOutput:    "Error: deployment \"test\" does not exist\n",
		},
		{
			description: "service without deployment",
			args: strings.Split("experimental add-to-mesh service dummyservice --meshConfigFile testdata/mesh-config.yaml"+
				" --injectConfigFile testdata/inject-config.yaml"+
				" --valuesFile testdata/inject-values.yaml", " "),
			namespace:         "default",
			expectedException: false,
			k8sConfigs:        cannedK8sConfigs,
			expectedOutput:    "No deployments found for service dummyservice.default\n",
		},
		{
			description:       "Invalid command args - missing service name",
			args:              strings.Split("experimental add-to-mesh service", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting service name\n",
		},
		{
			description:       "Invalid command args - missing service IP",
			args:              strings.Split("experimental add-to-mesh external-service test tcp:12345", " "),
			expectedException: true,
			expectedOutput:    "Error: provide service name, IP and Port List\n",
		},
		{
			description:       "Invalid command args - missing service Ports",
			args:              strings.Split("experimental add-to-mesh external-service test 172.186.15.123", " "),
			expectedException: true,
			expectedOutput:    "Error: provide service name, IP and Port List\n",
		},
		{
			description:       "Invalid command args - invalid port protocol",
			args:              strings.Split("experimental add-to-mesh external-service test 172.186.15.123 tcp1:12345", " "),
			expectedException: true,
			expectedOutput:    "Error: protocol tcp1 is not supported by Istio\n",
		},
		{
			description:       "service already exists",
			args:              strings.Split("experimental add-to-mesh external-service dummyservice 11.11.11.11 tcp:12345", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfigs,
			dynamicConfigs:    cannedDynamicConfigs,
			namespace:         "default",
			expectedOutput:    "Error: service \"dummyservice\" already exists, skip\n",
		},
		{
			description:       "service already exists (with short syntax)",
			args:              strings.Split("x add es dummyservice 11.11.11.11 tcp:12345", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfigs,
			dynamicConfigs:    cannedDynamicConfigs,
			namespace:         "default",
			expectedOutput:    "Error: service \"dummyservice\" already exists, skip\n",
		},
		{
			description:       "ServiceEntry already exists",
			args:              strings.Split("experimental add-to-mesh external-service vmtest 11.11.11.11 tcp:12345", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfigs,
			dynamicConfigs:    cannedDynamicConfigs,
			namespace:         "default",
			expectedOutput:    "Error: service entry \"mesh-expansion-vmtest\" already exists, skip\n",
		},
		{
			description:    "external service banana namespace",
			args:           strings.Split("experimental add-to-mesh external-service vmtest 11.11.11.11 tcp:12345 tcp:12346", " "),
			k8sConfigs:     cannedK8sConfigs,
			dynamicConfigs: cannedDynamicConfigs,
			namespace:      "banana",
			expectedOutput: `ServiceEntry "mesh-expansion-vmtest.banana" has been created in the Istio service mesh for the external service "vmtest"
Kubernetes Service "vmtest.banana" has been created in the Istio service mesh for the external service "vmtest"
`,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.description), func(t *testing.T) {
			verifyAddToMeshOutput(t, c)
		})
	}
}

func verifyAddToMeshOutput(t *testing.T, c testcase) {
	t.Helper()

	interfaceFactory = mockInterfaceFactoryGenerator(c.k8sConfigs)
	crdFactory = mockDynamicClientGenerator(c.dynamicConfigs)
	var out bytes.Buffer
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	if c.namespace != "" {
		namespace = c.namespace
	}

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedException {
		if fErr == nil {
			t.Fatalf("Wanted an exception, "+
				"didn't get one, output was %q", output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception: %v", fErr)
		}
	}

	if c.expectedOutput != "" && c.expectedOutput != output {
		assert.Equal(t, c.expectedOutput, output)
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}
}

func mockDynamicClientGenerator(dynamicConfigs []runtime.Object) func(kubeconfig string) (dynamic.Interface, error) {
	outFactory := func(_ string) (dynamic.Interface, error) {
		types := runtime.NewScheme()
		client := fake.NewSimpleDynamicClient(types, dynamicConfigs...)
		return client, nil
	}
	return outFactory
}

func TestSplitEqual(t *testing.T) {
	tests := []struct {
		arg       string
		wantKey   string
		wantValue string
	}{
		{arg: "key=value", wantKey: "key", wantValue: "value"},
		{arg: "key==value", wantKey: "key", wantValue: "=value"},
		{arg: "key=", wantKey: "key", wantValue: ""},
		{arg: "key", wantKey: "key", wantValue: ""},
		{arg: "", wantKey: "", wantValue: ""},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			gotKey, gotValue := splitEqual(tt.arg)
			if gotKey != tt.wantKey {
				t.Errorf("splitEqual(%v) got = %v, want %v", tt.arg, gotKey, tt.wantKey)
			}
			if gotValue != tt.wantValue {
				t.Errorf("splitEqual(%v) got1 = %v, want %v", tt.arg, gotValue, tt.wantValue)
			}
		})
	}
}

func TestConvertToMap(t *testing.T) {
	tests := []struct {
		name string
		arg  []string
		want map[string]string
	}{
		{name: "empty", arg: []string{""}, want: map[string]string{"": ""}},
		{name: "one-valid", arg: []string{"key=value"}, want: map[string]string{"key": "value"}},
		{name: "one-valid-double-equals", arg: []string{"key==value"}, want: map[string]string{"key": "=value"}},
		{name: "one-key-only", arg: []string{"key"}, want: map[string]string{"key": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertToStringMap(tt.arg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertToStringMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStr2NamedPort(t *testing.T) {
	tests := []struct {
		input  string    // input
		expVal namedPort // output
		expErr bool      // error
	}{
		// Good cases:
		{"http:5555", namedPort{5555, "http"}, false},
		{"80", namedPort{80, "http"}, false},
		{"443", namedPort{443, "https"}, false},
		{"1234", namedPort{1234, "1234"}, false},
		// Error cases:
		{"", namedPort{0, ""}, true},
		{"foo:bar", namedPort{0, "foo"}, true},
	}
	for _, tst := range tests {
		actVal, actErr := str2NamedPort(tst.input)
		if tst.expVal != actVal {
			t.Errorf("Got '%+v', expecting '%+v' for Str2NamedPort('%s')", actVal, tst.expVal, tst.input)
		}
		if tst.expErr {
			if actErr == nil {
				t.Errorf("Got no error when expecting an error for Str2NamedPort('%s')", tst.input)
			}
		} else {
			if actErr != nil {
				t.Errorf("Got unexpected error '%+v' when expecting none for Str2NamedPort('%s')", actErr, tst.input)
			}
		}
	}
}
