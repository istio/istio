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

package ingress

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	knetworking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

type fakeXdsUpdater struct {
	Events chan sets.Set[model.ConfigKey]
}

var _ xdsConfigUpdater = (*fakeXdsUpdater)(nil)

func (f *fakeXdsUpdater) ConfigUpdate(pr *model.PushRequest) {
	f.Events <- pr.ConfigsUpdated
}

func newFakeXds() *fakeXdsUpdater {
	return &fakeXdsUpdater{
		Events: make(chan sets.Set[model.ConfigKey], 100),
	}
}

func setupController(t *testing.T, domainPrefix string, objs ...runtime.Object) (*Controller, kube.Client) {
	kc := kube.NewFakeClient(objs...)
	stop := test.NewStop(t)

	mc := &meshconfig.MeshConfig{
		IngressControllerMode: meshconfig.MeshConfig_DEFAULT,
	}

	meshHolder := meshwatcher.NewTestWatcher(mc)
	controller := NewController(
		kc,
		meshHolder,
		kubecontroller.Options{
			DomainSuffix: domainPrefix,
			KrtDebugger:  krt.GlobalDebugHandler,
		},
		newFakeXds())
	kc.RunAndWait(stop)
	go controller.Run(stop)
	kube.WaitForCacheSync("test", stop, controller.HasSynced)

	return controller, kc
}

// vsNames returns the sorted set of VS names from the controller's output collection.
func vsNames(controller *Controller) sets.String {
	return sets.New(slices.Map(controller.outputs.VirtualServices.List(), func(c config.Config) string {
		return c.Name
	})...)
}

// waitForGatewayEvent waits for a Gateway push event from the xdsUpdater.
func waitForGatewayEvent(t *testing.T, xdsUpdater *fakeXdsUpdater, wanted sets.Set[model.ConfigKey]) {
	t.Helper()
	select {
	case x := <-xdsUpdater.Events:
		if !x.Equals(wanted) {
			t.Errorf("received unexpected gateway configs want: %v, got: %v", wanted, x)
		}
	case <-time.After(time.Second * 10):
		t.Fatalf("timed out waiting for gateway event")
	}
}

func TestIngressController(t *testing.T) {
	ingress1 := knetworking.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "mock", // goes into backend full name
			Name:      "test",
		},
		Spec: knetworking.IngressSpec{
			Rules: []knetworking.IngressRule{
				{
					Host: "my.host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/test",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "foo",
											Port: knetworking.ServiceBackendPort{
												Number: 8000,
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Host: "my2.host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/test1.*",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "bar",
											Port: knetworking.ServiceBackendPort{
												Number: 8000,
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Host: "my3.host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/test/*",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "bar",
											Port: knetworking.ServiceBackendPort{
												Number: 8000,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	ingress2 := knetworking.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "mock",
			Name:      "test",
		},
		Spec: knetworking.IngressSpec{
			Rules: []knetworking.IngressRule{
				{
					Host: "my.host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/test2",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "foo",
											Port: knetworking.ServiceBackendPort{
												Number: 8000,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	controller, client := setupController(t, "")
	xdsUpdater := controller.xdsUpdater.(*fakeXdsUpdater)
	ingress := clienttest.NewWriter[*knetworking.Ingress](t, client)

	ingress.Create(&ingress1)

	// Verify gateway push event
	waitForGatewayEvent(t, xdsUpdater, sets.New(
		model.ConfigKey{
			Kind:      kind.Gateway,
			Name:      ingress1.Name + "-" + constants.IstioIngressGatewayName + "-" + ingress1.Namespace,
			Namespace: IngressNamespace,
		},
	))

	// Verify virtual services are produced in the output collection
	assert.EventuallyEqual(t, func() sets.String {
		return vsNames(controller)
	}, sets.New(
		"my-host-com"+"-"+ingress1.Name+"-"+constants.IstioIngressGatewayName,
		"my2-host-com"+"-"+ingress1.Name+"-"+constants.IstioIngressGatewayName,
		"my3-host-com"+"-"+ingress1.Name+"-"+constants.IstioIngressGatewayName,
	), retry.Timeout(time.Second*10))

	ingress.Update(&ingress2)

	// After update, only 1 host remains
	assert.EventuallyEqual(t, func() sets.String {
		return vsNames(controller)
	}, sets.New(
		"my-host-com"+"-"+ingress2.Name+"-"+constants.IstioIngressGatewayName,
	), retry.Timeout(time.Second*10))
}

func TestIngressControllerWithPortName(t *testing.T) {
	ingressConfig := knetworking.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "mock",
			Name:      "test",
		},
		Spec: knetworking.IngressSpec{
			Rules: []knetworking.IngressRule{
				{
					Host: "my.host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/foo",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "foo",
											Port: knetworking.ServiceBackendPort{
												Number: 8000,
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Host: "my2.host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/bar",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "bar",
											Port: knetworking.ServiceBackendPort{
												Name: "http",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	serviceConfig := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "mock",
			Name:      "bar",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: 8080,
				},
			},
		},
	}

	controller, client := setupController(t, "")
	xdsUpdater := controller.xdsUpdater.(*fakeXdsUpdater)
	ingress := clienttest.NewWriter[*knetworking.Ingress](t, client)
	service := clienttest.NewWriter[*corev1.Service](t, client)

	// First create ingress.
	ingress.Create(&ingressConfig)

	// Verify gateway push event
	waitForGatewayEvent(t, xdsUpdater, sets.New(
		model.ConfigKey{
			Kind:      kind.Gateway,
			Name:      ingressConfig.Name + "-" + constants.IstioIngressGatewayName + "-" + ingressConfig.Namespace,
			Namespace: IngressNamespace,
		},
	))

	// Verify virtual services are produced in the output collection
	assert.EventuallyEqual(t, func() sets.String {
		return vsNames(controller)
	}, sets.New(
		"my-host-com"+"-"+ingressConfig.Name+"-"+constants.IstioIngressGatewayName,
		"my2-host-com"+"-"+ingressConfig.Name+"-"+constants.IstioIngressGatewayName,
	), retry.Timeout(time.Second*10))

	// Then we create service — this resolves the named port for the my2 VS.
	service.Create(&serviceConfig)

	// Verify the VS with the named port is updated (port resolved to 8080).
	assert.EventuallyEqual(t, func() uint32 {
		for _, vs := range controller.outputs.VirtualServices.List() {
			if vs.Name == "my2-host-com"+"-"+ingressConfig.Name+"-"+constants.IstioIngressGatewayName {
				return getVSPort(vs)
			}
		}
		return 0
	}, uint32(8080), retry.Timeout(time.Second*10))

	// We change service port number.
	serviceConfig.Spec.Ports[0].Port = 8090
	service.Update(&serviceConfig)

	// Verify the VS is updated with the new port number.
	assert.EventuallyEqual(t, func() uint32 {
		for _, vs := range controller.outputs.VirtualServices.List() {
			if vs.Name == "my2-host-com"+"-"+ingressConfig.Name+"-"+constants.IstioIngressGatewayName {
				return getVSPort(vs)
			}
		}
		return 0
	}, uint32(8090), retry.Timeout(time.Second*10))
}

// getVSPort extracts the port number from the first route destination of a VirtualService.
func getVSPort(vs config.Config) uint32 {
	spec, ok := vs.Spec.(*networking.VirtualService)
	if !ok || len(spec.Http) == 0 {
		return 0
	}
	for _, route := range spec.Http {
		for _, dest := range route.Route {
			if dest.Destination != nil && dest.Destination.Port != nil {
				return dest.Destination.Port.Number
			}
		}
	}
	return 0
}
