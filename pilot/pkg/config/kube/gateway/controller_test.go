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
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	destinationmodel "istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	gatewayClassSpec = &k8s.GatewayClassSpec{
		ControllerName: k8s.GatewayController(features.ManagedGatewayController),
	}
	gatewaySpec = &k8s.GatewaySpec{
		GatewayClassName: "gwclass",
		Listeners: []k8s.Listener{
			{
				Name:          "default",
				Port:          9009,
				Protocol:      "HTTP",
				AllowedRoutes: &k8s.AllowedRoutes{Namespaces: &k8s.RouteNamespaces{From: func() *k8s.FromNamespaces { x := k8s.NamespacesFromAll; return &x }()}},
			},
		},
	}
	httpRouteSpec = &k8s.HTTPRouteSpec{
		CommonRouteSpec: k8s.CommonRouteSpec{ParentRefs: []k8s.ParentReference{{
			Name: "gwspec",
		}}},
		Hostnames: []k8s.Hostname{"test.cluster.local"},
	}

	expectedgw = &networking.Gateway{
		Servers: []*networking.Server{
			{
				Port: &networking.Port{
					Number:   9009,
					Name:     "default",
					Protocol: "HTTP",
				},
				Hosts: []string{"*/*"},
			},
		},
	}
)

var AlwaysReady = func(class schema.GroupVersionResource, stop <-chan struct{}) bool {
	return true
}

func setupController(t *testing.T, objs ...runtime.Object) *Controller {
	return setupControllerWithRevision(t, "", objs...)
}

func setupControllerWithRevision(t *testing.T, revision string, objs ...runtime.Object) *Controller {
	kc := kube.NewFakeClient(objs...)
	setupClientCRDs(t, kc)
	stop := test.NewStop(t)
	controller := NewController(
		kc,
		AlwaysReady,
		controller.Options{KrtDebugger: krt.GlobalDebugHandler, Revision: revision},
		nil)
	kc.RunAndWait(stop)
	go controller.Run(stop)
	cg := core.NewConfigGenTest(t, core.TestOptions{})
	controller.Reconcile(cg.PushContext())
	kube.WaitForCacheSync("test", stop, controller.HasSynced)

	return controller
}

func TestListInvalidGroupVersionKind(t *testing.T) {
	controller := setupController(t)

	typ := config.GroupVersionKind{Kind: "wrong-kind"}
	c := controller.List(typ, "ns1")
	assert.Equal(t, len(c), 0)
}

func TestGlobalDestinationSourcesIncludeKubernetesServices(t *testing.T) {
	controller := setupController(t, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "payments", Namespace: "apps", UID: "uid-1"},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.0.0.1", Ports: []corev1.ServicePort{{Name: "http", Port: 80}}},
	})
	sources := controller.DestinationSources()
	retry.UntilSuccessOrFail(t, func() error {
		if len(sources.Frontends.List()) != 1 {
			return fmt.Errorf("frontend source not compiled")
		}
		for _, definition := range sources.Definitions.List() {
			if definition.ID.Source.Kind == kind.Service && definition.ID.Source.Name == "payments" {
				return nil
			}
		}
		return fmt.Errorf("destination source not compiled")
	})
	externalID := destinationmodel.DefinitionID{Source: model.ConfigKey{Kind: kind.ServiceEntry, Namespace: "apps", Name: "external"}, Port: "http"}
	externalDefinitions := krt.NewStaticCollection(nil, []destinationmodel.DestinationDefinition{{ID: externalID}})
	externalBindings := krt.NewStaticCollection[destinationmodel.DestinationBinding](nil, nil)
	externalFrontends := krt.NewStaticCollection[destinationmodel.FrontendDefinition](nil, nil)
	index := controller.InitializeGlobalDestinationIndex(DestinationSources{
		Frontends: externalFrontends, Definitions: externalDefinitions, Bindings: externalBindings,
	})
	if index != controller.DestinationIndex() {
		t.Fatal("global destination index was not adopted by Gateway provider")
	}
	foundExternal := false
	for _, definition := range controller.DestinationSources().Definitions.List() {
		foundExternal = foundExternal || definition.ID == externalID
	}
	if !foundExternal {
		t.Fatal("external source was not joined into global destination definitions")
	}
	bindings := sources.Bindings.List()
	wantRuntimeName := sources.Frontends.List()[0].Hostname
	found := false
	for _, binding := range bindings {
		if binding.Definition.Source.Kind == kind.Service {
			found = binding.Consumer == (destinationmodel.ConsumerID{Kind: "Mesh"}) && binding.RuntimeName == wantRuntimeName
		}
	}
	if !found {
		t.Fatalf("Kubernetes frontend did not activate mesh destination: %+v", bindings)
	}
}

func TestListGatewayResourceType(t *testing.T) {
	controller := setupController(t,
		&k8s.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gwclass",
			},
			Spec: *gatewayClassSpec,
		},
		&k8s.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gwspec",
				Namespace: "ns1",
			},
			Spec: *gatewaySpec,
		},
		&k8s.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "http-route",
				Namespace: "ns1",
			},
			Spec: *httpRouteSpec,
		})

	dumpOnFailure(t, krt.GlobalDebugHandler)
	cfg := controller.List(gvk.Gateway, "ns1")
	assert.Equal(t, len(cfg), 1)
	for _, c := range cfg {
		assert.Equal(t, c.GroupVersionKind, gvk.Gateway)
		assert.Equal(t, c.Name, "gwspec"+"~"+constants.KubernetesGatewayName+"~default")
		assert.Equal(t, c.Namespace, "ns1")
		assert.Equal(t, c.Spec, any(expectedgw))
	}
}

// TestListGatewayResourceAcrossRevisions verifies that a Gateway whose istio.io/rev label
// points to a different revision is still emitted as config by this control plane. Without
// this behavior, changing the istio.io/rev label on a live Gateway causes the previously
// owning control plane to push empty xDS config to pods still running on the old revision,
// breaking traffic for ~30s during a canary upgrade. See issue #59959.
//
// Status writes for non-owning revisions are filtered separately in
// pilot/pkg/status/collections.go (RegisterStatus), and Deployment management is filtered in
// pilot/pkg/config/kube/gatewaycommon/deploymentcontroller.go, so cross-revision config
// emission does not cause status flapping or duplicate Deployment management.
func TestListGatewayResourceAcrossRevisions(t *testing.T) {
	controller := setupControllerWithRevision(t, "stable",
		&k8s.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gwclass",
			},
			Spec: *gatewayClassSpec,
		},
		// Gateway labeled for a *different* revision than the one we're running.
		// Pre-fix, this Gateway would be filtered out by tagWatcher.IsMine in
		// gateway_collection.go and produce no config, leading to an empty xDS push.
		&k8s.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "canary-gw",
				Namespace: "ns1",
				Labels: map[string]string{
					label.IoIstioRev.Name: "canary",
				},
			},
			Spec: *gatewaySpec,
		},
	)

	dumpOnFailure(t, krt.GlobalDebugHandler)
	cfg := controller.List(gvk.Gateway, "ns1")
	assert.Equal(t, len(cfg), 1)
	got := cfg[0]
	assert.Equal(t, got.GroupVersionKind, gvk.Gateway)
	assert.Equal(t, got.Name, "canary-gw"+"~"+constants.KubernetesGatewayName+"~default")
	assert.Equal(t, got.Namespace, "ns1")
	assert.Equal(t, got.Spec, any(expectedgw))
}
