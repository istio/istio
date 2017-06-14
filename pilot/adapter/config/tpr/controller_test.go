// Copyright 2017 Istio Authors
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

package tpr

import (
	"testing"
	"time"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube"
	"istio.io/pilot/proxy"
	"istio.io/pilot/test/mock"
	"istio.io/pilot/test/util"
)

func TestIngressController(t *testing.T) {
	cl, cleanup := makeTempClient(t)
	defer cleanup()

	mesh := proxy.DefaultMeshConfig()
	ctl := NewController(cl, &mesh, kube.ControllerOptions{
		Namespace:    cl.namespace,
		ResyncPeriod: resync,
	})

	stop := make(chan struct{})
	defer close(stop)
	go ctl.Run(stop)

	// Append an ingress notification handler that just counts number of notifications
	notificationCount := 0
	ctl.RegisterEventHandler(model.IngressRule, func(model.Config, model.Event) {
		notificationCount++
	})

	// Create an ingress resource of a different class,
	// So that we can later verify it doesn't generate a notification,
	// nor returned with List(), Get() etc.
	nginxIngress := v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "nginx-ingress",
			Namespace: cl.namespace,
			Annotations: map[string]string{
				kube.IngressClassAnnotation: "nginx",
			},
		},
		Spec: v1beta1.IngressSpec{
			Backend: &v1beta1.IngressBackend{
				ServiceName: "service1",
				ServicePort: intstr.FromInt(80),
			},
		},
	}
	createIngress(&nginxIngress, cl.client, t)

	// Create a "real" ingress resource, with 4 host/path rules and an additional "default" rule.
	const expectedRuleCount = 5
	ingress := v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: cl.namespace,
			Annotations: map[string]string{
				kube.IngressClassAnnotation: mesh.IngressClass,
			},
		},
		Spec: v1beta1.IngressSpec{
			Backend: &v1beta1.IngressBackend{
				ServiceName: "default-service",
				ServicePort: intstr.FromInt(80),
			},
			Rules: []v1beta1.IngressRule{
				{
					Host: "host1.com",
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Path: "/path1",
									Backend: v1beta1.IngressBackend{
										ServiceName: "service1",
										ServicePort: intstr.FromInt(80),
									},
								},
								{
									Path: "/path2",
									Backend: v1beta1.IngressBackend{
										ServiceName: "service2",
										ServicePort: intstr.FromInt(80),
									},
								},
							},
						},
					},
				},
				{
					Host: "host2.com",
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Path: "/path3",
									Backend: v1beta1.IngressBackend{
										ServiceName: "service3",
										ServicePort: intstr.FromInt(80),
									},
								},
								{
									Path: "/path4",
									Backend: v1beta1.IngressBackend{
										ServiceName: "service4",
										ServicePort: intstr.FromInt(80),
									},
								},
							},
						},
					},
				},
			},
		},
	}
	createIngress(&ingress, cl.client, t)

	util.Eventually(func() bool {
		return notificationCount == expectedRuleCount
	}, t)
	if notificationCount != expectedRuleCount {
		t.Errorf("expected %d IngressRule events to be notified, found %d", expectedRuleCount, notificationCount)
	}

	util.Eventually(func() bool {
		rules, _ := ctl.List(model.IngressRule)
		return len(rules) == expectedRuleCount
	}, t)
	rules, err := ctl.List(model.IngressRule)
	if err != nil {
		t.Errorf("ctl.List(model.IngressRule, %s) => error: %v", cl.namespace, err)
	}
	if len(rules) != expectedRuleCount {
		t.Errorf("expected %d IngressRule objects to be created, found %d", expectedRuleCount, len(rules))
	}

	for _, listMsg := range rules {
		getMsg, exists, _ := ctl.Get(model.IngressRule, listMsg.Key)
		if !exists {
			t.Errorf("expected IngressRule with key %v to exist", listMsg.Key)

			listRule := listMsg.Content.(*proxyconfig.IngressRule)
			getRule := getMsg.(*proxyconfig.IngressRule)

			// TODO:  Compare listRule and getRule objects
			if listRule == nil {
				t.Errorf("expected listRule to be of type *proxyconfig.RouteRule")
			}
			if getRule == nil {
				t.Errorf("expected getRule to be of type *proxyconfig.RouteRule")
			}
		}
	}

}

func TestIngressClass(t *testing.T) {
	cl, cleanup := makeTempClient(t)
	defer cleanup()

	cases := []struct {
		ingressMode   proxyconfig.ProxyMeshConfig_IngressControllerMode
		ingressClass  string
		shouldProcess bool
	}{
		{ingressMode: proxyconfig.ProxyMeshConfig_DEFAULT, ingressClass: "nginx", shouldProcess: false},
		{ingressMode: proxyconfig.ProxyMeshConfig_STRICT, ingressClass: "nginx", shouldProcess: false},
		{ingressMode: proxyconfig.ProxyMeshConfig_DEFAULT, ingressClass: "istio", shouldProcess: true},
		{ingressMode: proxyconfig.ProxyMeshConfig_STRICT, ingressClass: "istio", shouldProcess: true},
		{ingressMode: proxyconfig.ProxyMeshConfig_DEFAULT, ingressClass: "", shouldProcess: true},
		{ingressMode: proxyconfig.ProxyMeshConfig_STRICT, ingressClass: "", shouldProcess: false},
	}

	for _, c := range cases {
		ingress := v1beta1.Ingress{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:        "test-ingress",
				Namespace:   "default",
				Annotations: make(map[string]string),
			},
			Spec: v1beta1.IngressSpec{
				Backend: &v1beta1.IngressBackend{
					ServiceName: "default-http-backend",
					ServicePort: intstr.FromInt(80),
				},
			},
		}

		mesh := proxy.DefaultMeshConfig()
		mesh.IngressControllerMode = c.ingressMode
		ctl := NewController(cl, &mesh, kube.ControllerOptions{
			Namespace:    cl.namespace,
			ResyncPeriod: resync,
		})

		if c.ingressClass != "" {
			ingress.Annotations["kubernetes.io/ingress.class"] = c.ingressClass
		}

		if c.shouldProcess != ctl.shouldProcessIngress(&ingress) {
			t.Errorf("shouldProcessIngress(<ingress of class '%s'>) => %v, want %v",
				c.ingressClass, !c.shouldProcess, c.shouldProcess)
		}
	}
}

func TestController(t *testing.T) {
	cl, cleanup := makeTempClient(t)
	defer cleanup()
	mesh := proxy.DefaultMeshConfig()
	ctl := NewController(cl, &mesh, kube.ControllerOptions{Namespace: cl.namespace, ResyncPeriod: resync})
	mock.CheckCacheEvents(cl, ctl, 5, t)
}

func TestControllerCacheFreshness(t *testing.T) {
	cl, cleanup := makeTempClient(t)
	defer cleanup()
	mesh := proxy.DefaultMeshConfig()
	ctl := NewController(cl, &mesh, kube.ControllerOptions{Namespace: cl.namespace, ResyncPeriod: resync})
	mock.CheckCacheFreshness(ctl, t)
}

func TestControllerClientSync(t *testing.T) {
	cl, cleanup := makeTempClient(t)
	defer cleanup()
	mesh := proxy.DefaultMeshConfig()
	ctl := NewController(cl, &mesh, kube.ControllerOptions{Namespace: cl.namespace, ResyncPeriod: resync})
	mock.CheckCacheSync(cl, ctl, 5, t)
}

const (
	resync = 1 * time.Second
)

func createIngress(ingress *v1beta1.Ingress, client kubernetes.Interface, t *testing.T) {
	if _, err := client.ExtensionsV1beta1().Ingresses(ingress.Namespace).Create(ingress); err != nil {
		t.Errorf("Cannot create ingress in namespace %s (error: %v)", ingress.Namespace, err)
	}
}
