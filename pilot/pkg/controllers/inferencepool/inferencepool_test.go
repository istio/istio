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

package inferencepool

import (
	"testing"

	"go.yaml.in/yaml/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestReconcileInferencePool(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIInferenceExtension, true)
	pool := &inferencev1.InferencePool{
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
	}
	controller := setupController(t,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		NewGateway("test-gw", InNamespace(DefaultTestNS), WithGatewayClass("istio")),
		NewHTTPRoute("test-route", InNamespace(DefaultTestNS),
			WithParentRefAndStatus("test-gw", DefaultTestNS, IstioController),
			WithBackendRef("test-pool", DefaultTestNS),
		),
		pool,
	)

	dumpOnFailure(t, krt.GlobalDebugHandler)

	// Verify the service was created
	var service *corev1.Service
	assert.EventuallyEqual(t, func() bool {
		svcName, err := model.InferencePoolServiceName("test-pool")
		if err != nil {
			return false
		}
		service, err = controller.client.Kube().CoreV1().Services("default").Get(t.Context(), svcName, metav1.GetOptions{})
		if err != nil {
			t.Logf("Service %s not found yet: %v", svcName, err)
			return false
		}
		return service != nil
	}, true)

	assert.Equal(t, service.ObjectMeta.Labels[constants.InternalServiceSemantics], constants.ServiceSemanticsInferencePool)
	assert.Equal(t, service.ObjectMeta.Labels[model.InferencePoolRefLabel], pool.Name)
	assert.Equal(t, service.OwnerReferences[0].Name, pool.Name)
	assert.Equal(t, service.Spec.Ports[0].TargetPort.IntVal, int32(8080))
	assert.Equal(t, service.Spec.Ports[0].Port, int32(54321)) // dummyPort + i
}

func dumpOnFailure(t *testing.T, debugger *krt.DebugHandler) {
	t.Cleanup(func() {
		if t.Failed() {
			b, _ := yaml.Marshal(debugger)
			t.Log(string(b))
		}
	})
}
