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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeVersion "k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	inferencev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
)

func TestReconcileInferencePool(t *testing.T) {
	discoveryNamespacesFilter := buildFilter("default")
	defaultNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	client := kube.NewFakeClient(defaultNamespace)
	kube.SetObjectFilter(client, discoveryNamespacesFilter)
	pool := &inferencev1alpha2.InferencePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool",
			Namespace: "default",
		},
		Spec: inferencev1alpha2.InferencePoolSpec{
			TargetPortNumber: 8080,
			Selector: map[inferencev1alpha2.LabelKey]inferencev1alpha2.LabelValue{
				"app": "test",
			},
			EndpointPickerConfig: inferencev1alpha2.EndpointPickerConfig{
				ExtensionRef: &inferencev1alpha2.Extension{
					ExtensionReference: inferencev1alpha2.ExtensionReference{
						Name: "dummy",
						// Kind:       &inferencev1alpha2.Kind(),
						PortNumber: ptr.Of(inferencev1alpha2.PortNumber(5421)),
					},
				},
			},
		},
	}
	client.Kube().Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &kubeVersion.Info{Major: "1", Minor: "28"}
	kclient.NewWriteClient[*inferencev1alpha2.InferencePool](client).Create(pool)
	stop := test.NewStop(t)

	controller := NewInferencePoolController(client)
	client.RunAndWait(stop)

	go controller.Run(stop)
	kube.WaitForCacheSync("test", stop, controller.queue.HasSynced)

	// Verify the service was created
	service := controller.services.Get("test-pool-ip-"+generateHash("test-pool", hashSize), "default")
	assert.NotNil(t, service)

	assert.Equal(t, service.ObjectMeta.Labels[constants.InternalServiceSemantics], constants.ServiceSemanticsInferencePool)
	assert.Equal(t, service.ObjectMeta.Labels[InferencePoolRefLabel], pool.Name)
	assert.Equal(t, service.OwnerReferences[0].Name, pool.Name)
	assert.Equal(t, service.Spec.Ports[0].TargetPort.IntVal, int32(8080))
}
