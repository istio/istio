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
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/pkg/config/schema/collections"
)

var (
	cannedK8sConfig = []runtime.Object{
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
								{Name: "istio-proxy", Image: "docker.io/istio/proxyv2:1.2.2"},
							},
							InitContainers: []coreV1.Container{
								{Name: "istio-init", Image: "docker.io/istio/proxy_init:1.2.2"},
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
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "vmtest",
					Namespace: "default",
				},
				Spec: coreV1.ServiceSpec{
					Ports: []coreV1.ServicePort{
						{
							Port: 9999,
							Name: "http",
						},
					},
					Selector: map[string]string{"app": "vmtest"},
				},
			},
		}},
	}
	cannedDynamicConfig = []runtime.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "networking.istio.io/" + collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Version(),
				"kind":       collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Kind(),
				"metadata": map[string]interface{}{
					"namespace": "default",
					"name":      "mesh-expansion-vmtest",
				},
			},
		},
	}
)

func TestRemoveFromMesh(t *testing.T) {
	cases := []testcase{
		{
			description:       "Invalid command args - missing service name",
			args:              strings.Split("experimental remove-from-mesh service", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting service name\n",
		},
		{
			description:       "Invalid command args - missing deployment name",
			args:              strings.Split("experimental remove-from-mesh deployment", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting deployment name\n",
		},
		{
			description:       "valid case - remove service from mesh",
			args:              strings.Split("experimental remove-from-mesh service details", " "),
			expectedException: false,
			k8sConfigs:        cannedK8sConfig,
			namespace:         "default",
			expectedOutput:    "deployment \"details-v1.default\" updated successfully with Istio sidecar un-injected.\n",
		},
		{
			description:       "valid case - remove deployment from mesh",
			args:              strings.Split("experimental remove-from-mesh deployment details-v1", " "),
			expectedException: false,
			k8sConfigs:        cannedK8sConfig,
			namespace:         "default",
			expectedOutput:    "deployment \"details-v1.default\" updated successfully with Istio sidecar un-injected.\n",
		},
		{
			description:       "service does not exist",
			args:              strings.Split("experimental remove-from-mesh service test", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfig,
			expectedOutput:    "Error: service \"test\" does not exist, skip\n",
		},
		{
			description:       "deployment does not exist",
			args:              strings.Split("experimental remove-from-mesh deployment test", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfig,
			expectedOutput:    "Error: deployment \"test\" does not exist\n",
		},
		{
			description:       "service without deployment",
			args:              strings.Split("experimental remove-from-mesh service dummyservice", " "),
			expectedException: false,
			k8sConfigs:        cannedK8sConfig,
			namespace:         "default",
			expectedOutput:    "No deployments found for service dummyservice.default\n",
		},
		{
			description:       "Invalid command args - missing external service name",
			args:              strings.Split("experimental remove-from-mesh external-service", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting external service name\n",
		},
		{
			description:       "external-service does not exist",
			args:              strings.Split("experimental remove-from-mesh external-service test", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfig,
			dynamicConfigs:    cannedDynamicConfig,
			expectedOutput:    "Error: service \"test\" does not exist, skip\n",
		},
		{
			description:       "ServiceEntry does not exist",
			args:              strings.Split("experimental remove-from-mesh external-service dummyservice", " "),
			expectedException: true,
			k8sConfigs:        cannedK8sConfig,
			dynamicConfigs:    cannedDynamicConfig,
			namespace:         "default",
			expectedOutput:    "Error: service entry \"mesh-expansion-dummyservice\" does not exist, skip\n",
		},
		{
			description:       "valid case - external service",
			args:              strings.Split("experimental remove-from-mesh external-service vmtest", " "),
			expectedException: false,
			k8sConfigs:        cannedK8sConfig,
			dynamicConfigs:    cannedDynamicConfig,
			namespace:         "default",
			expectedOutput: "Kubernetes Service \"vmtest.default\" has been deleted for external service \"vmtest\"\n" +
				"Service Entry \"mesh-expansion-vmtest\" has been deleted for external service \"vmtest\"\n",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.description), func(t *testing.T) {
			verifyAddToMeshOutput(t, c)
		})
	}
}
