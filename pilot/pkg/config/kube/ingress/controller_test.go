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

	net "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
)

func newFakeController() (model.ConfigStoreController, kube.Client) {
	meshHolder := mesh.NewTestWatcher(&meshconfig.MeshConfig{
		IngressControllerMode: meshconfig.MeshConfig_DEFAULT,
	})
	fakeClient := kube.NewFakeClient()
	return NewController(fakeClient, meshHolder, kubecontroller.Options{}), fakeClient
}

func TestIngressController(t *testing.T) {
	ingress1 := net.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "mock", // goes into backend full name
			Name:      "test",
		},
		Spec: net.IngressSpec{
			Rules: []net.IngressRule{
				{
					Host: "my.host.com",
					IngressRuleValue: net.IngressRuleValue{
						HTTP: &net.HTTPIngressRuleValue{
							Paths: []net.HTTPIngressPath{
								{
									Path: "/test",
									Backend: net.IngressBackend{
										Service: &net.IngressServiceBackend{
											Name: "foo",
											Port: net.ServiceBackendPort{
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
					IngressRuleValue: net.IngressRuleValue{
						HTTP: &net.HTTPIngressRuleValue{
							Paths: []net.HTTPIngressPath{
								{
									Path: "/test1.*",
									Backend: net.IngressBackend{
										Service: &net.IngressServiceBackend{
											Name: "bar",
											Port: net.ServiceBackendPort{
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
					IngressRuleValue: net.IngressRuleValue{
						HTTP: &net.HTTPIngressRuleValue{
							Paths: []net.HTTPIngressPath{
								{
									Path: "/test/*",
									Backend: net.IngressBackend{
										Service: &net.IngressServiceBackend{
											Name: "bar",
											Port: net.ServiceBackendPort{
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

	ingress2 := net.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "mock",
			Name:      "test",
		},
		Spec: net.IngressSpec{
			Rules: []net.IngressRule{
				{
					Host: "my.host.com",
					IngressRuleValue: net.IngressRuleValue{
						HTTP: &net.HTTPIngressRuleValue{
							Paths: []net.HTTPIngressPath{
								{
									Path: "/test2",
									Backend: net.IngressBackend{
										Service: &net.IngressServiceBackend{
											Name: "foo",
											Port: net.ServiceBackendPort{
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

	controller, client := newFakeController()
	ingress := clienttest.NewWriter[*net.Ingress](t, client)
	configCh := make(chan config.Config)

	configHandler := func(_, curr config.Config, event model.Event) {
		configCh <- curr
	}

	wait := func() config.Config {
		select {
		case x := <-configCh:
			return x
		case <-time.After(time.Second * 10):
			t.Fatalf("timed out waiting for config")
		}
		return config.Config{}
	}

	controller.RegisterEventHandler(gvk.VirtualService, configHandler)
	stopCh := make(chan struct{})
	go controller.Run(stopCh)
	defer close(stopCh)

	client.RunAndWait(stopCh)

	ingress.Create(&ingress1)
	vs := wait()
	if vs.Name != ingress1.Name+"-"+"virtualservice" || vs.Namespace != ingress1.Namespace {
		t.Errorf("received unecpected config %v/%v", vs.Namespace, vs.Name)
	}
	ingress.Update(&ingress2)
	vs = wait()
	if vs.Name != ingress1.Name+"-"+"virtualservice" || vs.Namespace != ingress1.Namespace {
		t.Errorf("received unecpected config %v/%v", vs.Namespace, vs.Name)
	}
}
