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

package gateway

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestReconcileInferencePool(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIInferenceExtension, true)

	testCases := []struct {
		name                string
		inferencePool       *inferencev1.InferencePool
		shadowService       *corev1.Service // name is optional, if not provided, it will be generated
		expectedAnnotations map[string]string
		expectedLabels      map[string]string
		expectedServiceName string
		expectedTargetPorts []int32
	}{
		{
			name: "basic shadow service creation",
			inferencePool: &inferencev1.InferencePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pool",
					Namespace: "default",
				},
				Spec: inferencev1.InferencePoolSpec{
					TargetPorts: []inferencev1.Port{
						{
							Number: inferencev1.PortNumber(8080),
						},
					},
					Selector: inferencev1.LabelSelector{
						MatchLabels: map[inferencev1.LabelKey]inferencev1.LabelValue{
							"app": "test",
						},
					},
					EndpointPickerRef: inferencev1.EndpointPickerRef{
						Name: "dummy",
						Port: &inferencev1.Port{
							Number: inferencev1.PortNumber(5421),
						},
					},
				},
			},
			expectedLabels: map[string]string{
				constants.InternalServiceSemantics: constants.ServiceSemanticsInferencePool,
				InferencePoolRefLabel:              "test-pool",
			},
			expectedTargetPorts: []int32{8080},
		},
		{
			name: "user label and annotation preservation",
			inferencePool: &inferencev1.InferencePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "preserve-pool",
					Namespace: "default",
				},
				Spec: inferencev1.InferencePoolSpec{
					TargetPorts: []inferencev1.Port{
						{
							Number: inferencev1.PortNumber(8080),
						},
					},
					Selector: inferencev1.LabelSelector{
						MatchLabels: map[inferencev1.LabelKey]inferencev1.LabelValue{
							"app": "test",
						},
					},
					EndpointPickerRef: inferencev1.EndpointPickerRef{
						Name: "dummy",
						Port: &inferencev1.Port{
							Number: inferencev1.PortNumber(5421),
						},
					},
				},
			},
			shadowService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						InferencePoolRefLabel:       "preserve-pool",
						"user.example.com/my-label": "user-value",
						"another.domain.com/label":  "another-value",
					},
					Annotations: map[string]string{
						"user.example.com/my-annotation": "user-annotation-value",
						"monitoring.example.com/scrape":  "true",
					},
				},
				Spec: corev1.ServiceSpec{
					Selector:  map[string]string{"app": "test"},
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: corev1.ClusterIPNone,
					Ports: []corev1.ServicePort{
						{
							Protocol:   "TCP",
							Port:       54321,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			},
			expectedAnnotations: map[string]string{
				"user.example.com/my-annotation": "user-annotation-value",
				"monitoring.example.com/scrape":  "true",
			},
			expectedLabels: map[string]string{
				constants.InternalServiceSemantics: constants.ServiceSemanticsInferencePool,
				InferencePoolRefLabel:              "preserve-pool",
				"user.example.com/my-label":        "user-value",
				"another.domain.com/label":         "another-value",
			},
			expectedTargetPorts: []int32{8080},
		},
		{
			name: "very long inferencepool name",
			inferencePool: &inferencev1.InferencePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "very-long-inference-pool-name-that-should-be-truncated-properly",
					Namespace: "default",
				},
				Spec: inferencev1.InferencePoolSpec{
					TargetPorts: []inferencev1.Port{
						{
							Number: inferencev1.PortNumber(9090),
						},
					},
					Selector: inferencev1.LabelSelector{
						MatchLabels: map[inferencev1.LabelKey]inferencev1.LabelValue{
							"app": "longname",
						},
					},
					EndpointPickerRef: inferencev1.EndpointPickerRef{
						Name: "dummy",
						Port: &inferencev1.Port{
							Number: inferencev1.PortNumber(5421),
						},
					},
				},
			},
			expectedLabels: map[string]string{
				constants.InternalServiceSemantics: constants.ServiceSemanticsInferencePool,
				InferencePoolRefLabel:              "very-long-inference-pool-name-that-should-be-truncated-properly",
			},
			expectedServiceName: "very-long-inference-pool-name-that-should-be-trunca-ip-6d24df6a",
			expectedTargetPorts: []int32{9090},
		},
		{
			name: "multiple target ports creates single service port",
			inferencePool: &inferencev1.InferencePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-port-pool",
					Namespace: "default",
				},
				Spec: inferencev1.InferencePoolSpec{
					TargetPorts: []inferencev1.Port{
						{
							Number: inferencev1.PortNumber(8000),
						},
						{
							Number: inferencev1.PortNumber(8001),
						},
						{
							Number: inferencev1.PortNumber(8002),
						},
					},
					Selector: inferencev1.LabelSelector{
						MatchLabels: map[inferencev1.LabelKey]inferencev1.LabelValue{
							"app": "multiport",
						},
					},
					EndpointPickerRef: inferencev1.EndpointPickerRef{
						Name: "dummy",
						Port: &inferencev1.Port{
							Number: inferencev1.PortNumber(5421),
						},
					},
				},
			},
			expectedLabels: map[string]string{
				constants.InternalServiceSemantics: constants.ServiceSemanticsInferencePool,
				InferencePoolRefLabel:              "multi-port-pool",
			},
			expectedTargetPorts: []int32{8000, 8001, 8002},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			objects := []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				NewGateway(tc.name+"-gw", InNamespace("default"), WithGatewayClass("istio")),
				NewHTTPRoute(tc.name+"-route", InNamespace("default"),
					WithParentRefAndStatus(tc.name+"-gw", "default", "istio.io/gateway-controller"),
					WithBackendRef(tc.inferencePool.Name, "default"),
				),
				tc.inferencePool,
			}
			if tc.shadowService != nil {
				// Generate the service name if not provided
				if tc.shadowService.Name == "" {
					generatedName, err := InferencePoolServiceName(tc.inferencePool.Name)
					assert.NoError(t, err)
					tc.shadowService.Name = generatedName
				}
				objects = append(objects, tc.shadowService)
			}
			controller := setupController(t, objects...)

			var service *corev1.Service
			expectedSvcName, err := InferencePoolServiceName(tc.inferencePool.Name)
			if tc.expectedServiceName != "" {
				assert.Equal(t, expectedSvcName, tc.expectedServiceName, fmt.Sprintf("Service name should be '%s'", tc.expectedServiceName))
			}
			assert.NoError(t, err)
			assert.EventuallyEqual(t, func() bool {
				var err error
				service, err = controller.client.Kube().CoreV1().Services("default").Get(t.Context(), expectedSvcName, metav1.GetOptions{})
				if err != nil {
					t.Logf("Service %s not found yet: %v", expectedSvcName, err)
					return false
				}
				return service != nil && service.Labels[constants.InternalServiceSemantics] == constants.ServiceSemanticsInferencePool
			}, true)

			for key, expectedValue := range tc.expectedLabels {
				assert.Equal(t, service.Labels[key], expectedValue, fmt.Sprintf("Label '%s' should have value '%s'", key, expectedValue))
			}
			for key, expectedValue := range tc.expectedAnnotations {
				assert.Equal(t, service.Annotations[key], expectedValue, fmt.Sprintf("Annotation '%s' should have value '%s'", key, expectedValue))
			}
			expectedPortCount := len(tc.inferencePool.Spec.TargetPorts)
			assert.Equal(t, len(service.Spec.Ports), expectedPortCount, fmt.Sprintf("Shadow service should have %d ports", expectedPortCount))

			for i := 1; i < len(service.Spec.Ports); i++ {
				assert.Equal(t, service.Spec.Ports[i].Port, int32(54321+i))
				assert.Equal(t, service.Spec.Ports[i].TargetPort.IntVal, tc.expectedTargetPorts[i])
				assert.Equal(t, service.Spec.Ports[i].Name, fmt.Sprintf("http-%d", i))
			}

			assert.Equal(t, service.OwnerReferences[0].Name, tc.inferencePool.Name)
		})
	}
}
