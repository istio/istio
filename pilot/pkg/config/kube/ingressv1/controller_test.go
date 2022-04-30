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
	"context"
	"testing"
	"time"

	"k8s.io/api/networking/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	serviceregistrykube "istio.io/istio/pilot/pkg/serviceregistry/kube"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
)

var (
	ingressWithoutClass = []v1.Ingress{
		{
			ObjectMeta: metaV1.ObjectMeta{
				Namespace: "mock", // goes into backend full name
				Name:      "test-1",
			},
			Spec: v1.IngressSpec{
				Rules: []v1.IngressRule{
					{
						Host: "my.host.com",
						IngressRuleValue: v1.IngressRuleValue{
							HTTP: &v1.HTTPIngressRuleValue{
								Paths: []v1.HTTPIngressPath{
									{
										Path: "/test1",
										Backend: v1.IngressBackend{
											Service: &v1.IngressServiceBackend{
												Name: "foo",
												Port: v1.ServiceBackendPort{
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
		},
		{
			ObjectMeta: metaV1.ObjectMeta{
				Namespace: "mock", // goes into backend full name
				Name:      "test-2",
			},
			Spec: v1.IngressSpec{
				Rules: []v1.IngressRule{
					{
						Host: "my.host.com",
						IngressRuleValue: v1.IngressRuleValue{
							HTTP: &v1.HTTPIngressRuleValue{
								Paths: []v1.HTTPIngressPath{
									{
										Path: "/test2",
										Backend: v1.IngressBackend{
											Service: &v1.IngressServiceBackend{
												Name: "bar",
												Port: v1.ServiceBackendPort{
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
		},
	}

	ingressWithClass = []v1.Ingress{
		{
			ObjectMeta: metaV1.ObjectMeta{
				Namespace: "mock", // goes into backend full name
				Name:      "test-3",
				Annotations: map[string]string{
					serviceregistrykube.IngressClassAnnotation: "istio",
				},
			},
			Spec: v1.IngressSpec{
				Rules: []v1.IngressRule{
					{
						Host: "my.host.com",
						IngressRuleValue: v1.IngressRuleValue{
							HTTP: &v1.HTTPIngressRuleValue{
								Paths: []v1.HTTPIngressPath{
									{
										Path: "/test1",
										Backend: v1.IngressBackend{
											Service: &v1.IngressServiceBackend{
												Name: "foo",
												Port: v1.ServiceBackendPort{
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
		},
		{
			ObjectMeta: metaV1.ObjectMeta{
				Namespace: "mock", // goes into backend full name
				Name:      "test-4",
				Annotations: map[string]string{
					serviceregistrykube.IngressClassAnnotation: "istio",
				},
			},
			Spec: v1.IngressSpec{
				Rules: []v1.IngressRule{
					{
						Host: "my.host.com",
						IngressRuleValue: v1.IngressRuleValue{
							HTTP: &v1.HTTPIngressRuleValue{
								Paths: []v1.HTTPIngressPath{
									{
										Path: "/test2",
										Backend: v1.IngressBackend{
											Service: &v1.IngressServiceBackend{
												Name: "bar",
												Port: v1.ServiceBackendPort{
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
		},
	}
)

type ingressClassOptions struct {
	mode         meshconfig.MeshConfig_IngressControllerMode
	ingressClass string
}

type configHandler struct {
	notifyCh chan config.Config
}

func newConfigHandler() *configHandler{
	return &configHandler{
		notifyCh: make(chan config.Config, 2),
	}
}

func (c *configHandler) handle(_, curr config.Config, _ model.Event) {
	c.notifyCh <- curr
}

func newFakeController(options ingressClassOptions) (model.ConfigStoreController, kube.Client) {
	meshHolder := mesh.NewTestWatcher(&meshconfig.MeshConfig{
		IngressControllerMode: options.mode,
		IngressClass: options.ingressClass,
	})
	fakeClient := kube.NewFakeClient()
	return NewController(fakeClient, meshHolder, kubecontroller.Options{}), fakeClient
}

func initAndStartController(options ingressClassOptions, handler *configHandler,
	stopCh chan struct{}) (*controller, kube.Client) {
	cacheStoreController, client := newFakeController(options)
	cacheStoreController.RegisterEventHandler(gvk.VirtualService, handler.handle)

	go cacheStoreController.Run(stopCh)
	client.RunAndWait(stopCh)

	rawController := cacheStoreController.(*controller)
	rawController.eventCompletedCallback = func() {
		// Use empty config as event completed mark.
		handler.notifyCh <- config.Config{}
	}
	return rawController, client
}

func TestIngressControllerWithDefaultIngressControllerMode(t *testing.T) {
	configHandler := newConfigHandler()
	stopCh := make(chan struct{})
	defer close(stopCh)

	options := ingressClassOptions{
		mode: meshconfig.MeshConfig_DEFAULT,
	}
	controller, client := initAndStartController(options, configHandler, stopCh)

	wait := func() config.Config {
		result := config.Config{}
		for {
			select {
			case x := <-configHandler.notifyCh:
				if x.Name != "" {
					result = x
				} else {
					return result
				}
			case <-time.After(time.Second * 10):
				t.Fatalf("timed out waiting for config")
				return result
			}
		}
	}

	for _, ingress := range ingressWithoutClass {
		client.NetworkingV1().Ingresses(ingress.Namespace).Create(context.TODO(), &ingress, metaV1.CreateOptions{})
		vs := wait()
		if vs.Name != ingress.Name+"-"+"virtualservice" || vs.Namespace != ingress.Namespace {
			t.Fatalf("received unecpected config %v/%v", vs.Namespace, vs.Name)
		}
	}

	if len(controller.ingresses) != len(ingressWithoutClass) {
		t.Fatalf("ingresses size should be %d", len(ingressWithoutClass))
	}

	// Apply unmatched ingresses
	for _, ingress := range ingressWithClass {
		client.NetworkingV1().Ingresses(ingress.Namespace).Create(context.TODO(), &ingress, metaV1.CreateOptions{})
		vs := wait()
		if vs.Name != "" || vs.Namespace != "" {
			t.Fatalf("received unecpected config %v/%v", vs.Namespace, vs.Name)
		}
	}

	// We should not store unmatched ingresses
	if len(controller.ingresses) != len(ingressWithoutClass) {
		t.Fatalf("ingresses size should be %d, actual is %d", len(ingressWithoutClass), len(controller.ingresses))
	}
}

func TestIngressControllerWithStrictIngressControllerMode(t *testing.T) {
	configHandler := newConfigHandler()
	stopCh := make(chan struct{})
	defer close(stopCh)

	options := ingressClassOptions{
		mode: meshconfig.MeshConfig_STRICT,
		ingressClass: "istio",
	}
	controller, client := initAndStartController(options, configHandler, stopCh)

	wait := func() config.Config {
		result := config.Config{}
		for {
			select {
			case x := <-configHandler.notifyCh:
				if x.Name != "" {
					result = x
				} else {
					return result
				}
			case <-time.After(time.Second * 10):
				t.Fatalf("timed out waiting for config")
				return result
			}
		}
	}

	for _, ingress := range ingressWithClass {
		client.NetworkingV1().Ingresses(ingress.Namespace).Create(context.TODO(), &ingress, metaV1.CreateOptions{})
		vs := wait()
		if vs.Name != ingress.Name+"-"+"virtualservice" || vs.Namespace != ingress.Namespace {
			t.Fatalf("received unecpected config %v/%v", vs.Namespace, vs.Name)
		}
	}

	if len(controller.ingresses) != len(ingressWithClass) {
		t.Fatalf("ingresses size should be %d", len(ingressWithClass))
	}

	// Apply unmatched ingresses
	for _, ingress := range ingressWithoutClass {
		client.NetworkingV1().Ingresses(ingress.Namespace).Create(context.TODO(), &ingress, metaV1.CreateOptions{})
		vs := wait()
		if vs.Name != "" || vs.Namespace != "" {
			t.Fatalf("received unecpected config %v/%v", vs.Namespace, vs.Name)
		}
	}

	// We should not store unmatched ingresses
	if len(controller.ingresses) != len(ingressWithClass) {
		t.Fatalf("ingresses size should be %d, actual is %d", len(ingressWithClass), len(controller.ingresses))
	}
}

