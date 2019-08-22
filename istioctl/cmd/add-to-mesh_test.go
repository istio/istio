// Copyright 2019 Istio Authors.
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

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

type testcase struct {
	description       string
	expectedException bool
	args              []string
	k8sConfigs        []runtime.Object
	expectedOutput    string
}

var (
	one          = int32(1)
	cannedK8sEnv = []runtime.Object{
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
							Containers: []v1.Container{
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
)

func TestAddToMesh(t *testing.T) {
	cases := []testcase{
		{
			description:       "Invalid command args",
			args:              strings.Split("experimental add-to-mesh service", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting service name\n",
		},
		{
			description: "valid case",
			args: strings.Split("experimental add-to-mesh service details --meshConfigFile testdata/mesh-config.yaml"+
				" --injectConfigFile testdata/inject-config.yaml"+
				" --valuesFile testdata/inject-values.yaml", " "),
			expectedException: false,
			k8sConfigs:        cannedK8sEnv,
			expectedOutput: "deployment details-v1.default updated successfully with Istio sidecar injected.\n" +
				"Next Step: Add related labels to the deployment to align with Istio's requirement: " +
				"https://istio.io/docs/setup/kubernetes/additional-setup/requirements/\n",
		},
		{
			description: "service not exists",
			args: strings.Split("experimental add-to-mesh service test --meshConfigFile testdata/mesh-config.yaml"+
				" --injectConfigFile testdata/inject-config.yaml"+
				" --valuesFile testdata/inject-values.yaml", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sEnv,
			expectedOutput:    "Error: services \"test\" not found\n",
		},
		{
			description: "service without depolyment",
			args: strings.Split("experimental add-to-mesh service dummyservice --meshConfigFile testdata/mesh-config.yaml"+
				" --injectConfigFile testdata/inject-config.yaml"+
				" --valuesFile testdata/inject-values.yaml", " "),
			expectedException: false,
			k8sConfigs:        cannedK8sEnv,
			expectedOutput:    "No deployments found for service dummyservice.default\n",
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
	var out bytes.Buffer
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOutput(&out)

	file = "" // Clear, because we re-use

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedException {
		if fErr == nil {
			t.Fatalf("Wanted an exception,"+
				"didn't get one, output was %q", output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception: %v", fErr)
		}
	}

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}
}

func mockInterfaceFactoryGenerator(k8sConfigs []runtime.Object) func(kubeconfig string) (kubernetes.Interface, error) {
	outFactory := func(_ string) (kubernetes.Interface, error) {
		client := fake.NewSimpleClientset(k8sConfigs...)
		return client, nil
	}

	return outFactory
}
