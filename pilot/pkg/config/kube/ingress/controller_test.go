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

	"k8s.io/api/networking/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
)

func newFakeController() (model.ConfigStoreController, kube.Client) {
	meshHolder := mesh.NewTestWatcher(&meshconfig.MeshConfig{
		IngressControllerMode: meshconfig.MeshConfig_DEFAULT,
	})
	fakeClient := kube.NewFakeClient()
	return NewController(fakeClient, meshHolder, kubecontroller.Options{}), fakeClient
}

func TestIngressController(t *testing.T) {
	ingress1 := v1beta1.Ingress{
		ObjectMeta: metaV1.ObjectMeta{
			Namespace: "mock", // goes into backend full name
			Name:      "test",
		},
		Spec: v1beta1.IngressSpec{
			Rules: []v1beta1.IngressRule{
				{
					Host: "my.host.com",
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Path: "/test",
									Backend: v1beta1.IngressBackend{
										ServiceName: "foo",
										ServicePort: intstr.IntOrString{IntVal: 8000},
									},
								},
							},
						},
					},
				},
				{
					Host: "my2.host.com",
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Path: "/test1.*",
									Backend: v1beta1.IngressBackend{
										ServiceName: "bar",
										ServicePort: intstr.IntOrString{IntVal: 8000},
									},
								},
							},
						},
					},
				},
				{
					Host: "my3.host.com",
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Path: "/test/*",
									Backend: v1beta1.IngressBackend{
										ServiceName: "bar",
										ServicePort: intstr.IntOrString{IntVal: 8000},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	ingress2 := v1beta1.Ingress{
		ObjectMeta: metaV1.ObjectMeta{
			Namespace: "mock",
			Name:      "test",
		},
		Spec: v1beta1.IngressSpec{
			Rules: []v1beta1.IngressRule{
				{
					Host: "my.host.com",
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Path: "/test2",
									Backend: v1beta1.IngressBackend{
										ServiceName: "foo",
										ServicePort: intstr.IntOrString{IntVal: 8000},
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

	client.NetworkingV1beta1().Ingresses(ingress1.Namespace).Create(context.TODO(), &ingress1, metaV1.CreateOptions{})

	vs := wait()
	if vs.Name != ingress1.Name+"-"+"virtualservice" || vs.Namespace != ingress1.Namespace {
		t.Errorf("received unecpected config %v/%v", vs.Namespace, vs.Name)
	}
	client.NetworkingV1beta1().Ingresses(ingress2.Namespace).Update(context.TODO(), &ingress2, metaV1.UpdateOptions{})
	vs = wait()
	if vs.Name != ingress1.Name+"-"+"virtualservice" || vs.Namespace != ingress1.Namespace {
		t.Errorf("received unecpected config %v/%v", vs.Namespace, vs.Name)
	}
}
