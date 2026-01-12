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
	"istio.io/istio/pilot/pkg/model"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
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

	wait := func() (sets.Set[model.ConfigKey], kind.Kind) {
		select {
		case x := <-xdsUpdater.Events:
			return x, x.UnsortedList()[0].Kind
		case <-time.After(time.Second * 10):
			t.Fatalf("timed out waiting for config")
		}
		return nil, kind.Kind(0)
	}
	waitFor := func(t *testing.T, wanted map[kind.Kind]sets.Set[model.ConfigKey]) {
		for range len(wanted) {
			vs, k := wait()
			if w, ok := wanted[k]; ok {
				delete(wanted, k)
				if !vs.Equals(w) {
					t.Errorf("received unexpected configs want: %v, got: %v", w, vs)
				}
			} else {
				t.Errorf("received unexpected kind: %v", k)
			}
		}
	}

	ingress.Create(&ingress1)
	waitFor(t, map[kind.Kind]sets.Set[model.ConfigKey]{
		kind.VirtualService: sets.New(
			model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "my-host-com" + "-" + ingress1.Name + "-" + constants.IstioIngressGatewayName,
				Namespace: ingress1.Namespace,
			},
			model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "my2-host-com" + "-" + ingress1.Name + "-" + constants.IstioIngressGatewayName,
				Namespace: ingress1.Namespace,
			},
			model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "my3-host-com" + "-" + ingress1.Name + "-" + constants.IstioIngressGatewayName,
				Namespace: ingress1.Namespace,
			},
		),
		kind.Gateway: sets.New(
			model.ConfigKey{
				Kind:      kind.Gateway,
				Name:      ingress1.Name + "-" + constants.IstioIngressGatewayName + "-" + ingress1.Namespace,
				Namespace: IngressNamespace,
			},
		),
	})

	ingress.Update(&ingress2)
	waitFor(t, map[kind.Kind]sets.Set[model.ConfigKey]{
		kind.VirtualService: sets.New(
			model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "my-host-com" + "-" + ingress2.Name + "-" + constants.IstioIngressGatewayName,
				Namespace: ingress2.Namespace,
			},
			model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "my2-host-com" + "-" + ingress1.Name + "-" + constants.IstioIngressGatewayName,
				Namespace: ingress1.Namespace,
			},
			model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "my3-host-com" + "-" + ingress1.Name + "-" + constants.IstioIngressGatewayName,
				Namespace: ingress1.Namespace,
			},
		),
	})
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

	wait := func() (sets.Set[model.ConfigKey], kind.Kind) {
		select {
		case x := <-xdsUpdater.Events:
			return x, x.UnsortedList()[0].Kind
		case <-time.After(time.Second * 10):
			t.Fatalf("timed out waiting for config")
		}
		return nil, kind.Kind(0)
	}

	waitFor := func(t *testing.T, wanted map[kind.Kind]sets.Set[model.ConfigKey]) {
		for range len(wanted) {
			vs, k := wait()
			if w, ok := wanted[k]; ok {
				delete(wanted, k)
				if !vs.Equals(w) {
					t.Errorf("received unexpected configs want: %v, got: %v", w, vs)
				}
			} else {
				t.Errorf("received unexpected kind: %v", k)
			}
		}
	}

	// First create ingress.
	ingress.Create(&ingressConfig)
	waitFor(t, map[kind.Kind]sets.Set[model.ConfigKey]{
		kind.VirtualService: sets.New(
			model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "my-host-com" + "-" + ingressConfig.Name + "-" + constants.IstioIngressGatewayName,
				Namespace: ingressConfig.Namespace,
			},
			model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "my2-host-com" + "-" + ingressConfig.Name + "-" + constants.IstioIngressGatewayName,
				Namespace: ingressConfig.Namespace,
			},
		),
		kind.Gateway: sets.New(
			model.ConfigKey{
				Kind:      kind.Gateway,
				Name:      ingressConfig.Name + "-" + constants.IstioIngressGatewayName + "-" + ingressConfig.Namespace,
				Namespace: IngressNamespace,
			},
		),
	})

	// Then we create service.
	service.Create(&serviceConfig)
	waitFor(t, map[kind.Kind]sets.Set[model.ConfigKey]{
		kind.VirtualService: sets.New(
			model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "my2-host-com" + "-" + ingressConfig.Name + "-" + constants.IstioIngressGatewayName,
				Namespace: ingressConfig.Namespace,
			},
		),
	})

	// We change service port number.
	serviceConfig.Spec.Ports[0].Port = 8090
	service.Update(&serviceConfig)
	waitFor(t, map[kind.Kind]sets.Set[model.ConfigKey]{
		kind.VirtualService: sets.New(
			model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "my2-host-com" + "-" + ingressConfig.Name + "-" + constants.IstioIngressGatewayName,
				Namespace: ingressConfig.Namespace,
			},
		),
	})
}
