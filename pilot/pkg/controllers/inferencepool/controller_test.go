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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/gateway-api/pkg/consts"

	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
)

func setupClientCRDs(t *testing.T, kc kube.CLIClient) {
	for _, crd := range []schema.GroupVersionResource{
		gvr.KubernetesGateway,
		gvr.ReferenceGrant,
		gvr.XListenerSet,
		gvr.GatewayClass,
		gvr.HTTPRoute,
		gvr.GRPCRoute,
		gvr.TCPRoute,
		gvr.TLSRoute,
		gvr.ServiceEntry,
		gvr.XBackendTrafficPolicy,
		gvr.BackendTLSPolicy,
		gvr.InferencePool,
	} {
		clienttest.MakeCRDWithAnnotations(t, kc, crd, map[string]string{
			consts.BundleVersionAnnotation: "v1.1.0",
		})
	}
}

func setupController(t *testing.T, objs ...runtime.Object) *InferencePoolController {
	kc := kube.NewFakeClient(objs...)
	setupClientCRDs(t, kc)
	stop := test.NewStop(t)
	controller := NewController(
		kc,
		AlwaysReady,
		controller.Options{KrtDebugger: krt.GlobalDebugHandler},
	)
	kc.RunAndWait(stop)
	go controller.Run(stop)
	kube.WaitForCacheSync("test", stop, controller.HasSynced)

	return controller
}

var AlwaysReady = func(class schema.GroupVersionResource, stop <-chan struct{}) bool {
	return true
}
