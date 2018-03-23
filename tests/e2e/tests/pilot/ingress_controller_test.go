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

package pilot

import (
	"os"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"

	pb "istio.io/api/routing/v1alpha1"
	crd "istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/test/mock"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/log"
)

const (
	namespace = "test"
)

var (
	// Create an ingress resource of a different class,
	// So that we can later verify it doesn't generate a notification,
	// nor returned with List(), Get() etc.
	nginxIngress = v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "nginx-ingress",
			Namespace: namespace,
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
	ig = v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: namespace,
			Annotations: map[string]string{
				kube.IngressClassAnnotation: "istio",
			},
		},
		Spec: v1beta1.IngressSpec{
			Backend: &v1beta1.IngressBackend{
				ServiceName: "default-service",
				ServicePort: intstr.FromInt(80),
			},
			TLS: []v1beta1.IngressTLS{
				{
					Hosts:      []string{"host1.com"},
					SecretName: "my-secret1",
				},
				{
					Hosts:      []string{"host2.com"},
					SecretName: "my-secret2",
				},
			},
			Rules: []v1beta1.IngressRule{
				{
					Host: "host1.com",
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Path: "/path1/.*",
									Backend: v1beta1.IngressBackend{
										ServiceName: "service1",
										ServicePort: intstr.FromInt(80),
									},
								},
								{
									Path: "/path\\d",
									Backend: v1beta1.IngressBackend{
										ServiceName: "service2",
										ServicePort: intstr.FromString("http"),
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
				{
					Host: "host3.com",
				},
			},
		},
	}
)

func TestConfig(t *testing.T) {
	cl := fake.NewSimpleClientset()
	mesh := model.DefaultMeshConfig()
	ctl := crd.NewController(cl, &mesh, kube.ControllerOptions{
		WatchedNamespace: namespace,
		ResyncPeriod:     resync,
	})

	stop := make(chan struct{})
	go ctl.Run(stop)

	if len(ctl.ConfigDescriptor()) == 0 {
		t.Errorf("must support ingress type")
	}

	rule := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.IngressRule.Type,
			Name:      "test",
			Namespace: namespace,
		},
		Spec: mock.ExampleIngressRule,
	}

	// make sure all operations error out
	if _, err := ctl.Create(rule); err == nil {
		t.Errorf("Post should not be allowed")
	}

	if _, err := ctl.Update(rule); err == nil {
		t.Errorf("Put should not be allowed")
	}

	if err := ctl.Delete(model.IngressRule.Type, "test", namespace); err == nil {
		t.Errorf("Delete should not be allowed")
	}

	util.Eventually("HasSynced", ctl.HasSynced, t)
}

func TestIngressController(t *testing.T) {
	cl := fake.NewSimpleClientset()
	mesh := model.DefaultMeshConfig()
	ctl := crd.NewController(cl, &mesh, kube.ControllerOptions{
		WatchedNamespace: namespace,
		ResyncPeriod:     resync,
	})

	// Append an ingress notification handler that just counts number of notifications
	stop := make(chan struct{})
	count := make(chan bool)
	defer close(count)

	ctl.RegisterEventHandler(model.IngressRule.Type, func(config model.Config, ev model.Event) {
		count <- true
	})
	go ctl.Run(stop)

	if _, err := cl.ExtensionsV1beta1().Ingresses(nginxIngress.Namespace).Create(&nginxIngress); err != nil {
		t.Errorf("Cannot create ingress in namespace %s (error: %v)", nginxIngress.Namespace, err)
	}
	// Create a "real" ingress resource, with 4 host/path rules and an additional "default" rule.
	if _, err := cl.ExtensionsV1beta1().Ingresses(namespace).Create(&ig); err != nil {
		t.Errorf("Cannot create ingress in namespace %s (error: %v)", namespace, err)
	}

	const expectedRuleCount = 5
	timeout := time.After(3 * time.Second)
	actual := 0
	for {
		select {
		case <-count:
			actual++
		case <-timeout:
			t.Fatalf("timeout waiting to receive expected rule count %d, actually received %d",
				expectedRuleCount, actual)
		}
		if actual == expectedRuleCount {
			break
		}
	}

	rules, err := ctl.List(model.IngressRule.Type, namespace)
	if err != nil {
		t.Errorf("ctl.List(model.IngressRule, %s) => error: %v", namespace, err)
	}
	if len(rules) != expectedRuleCount {
		t.Errorf("expected %d IngressRule objects to be created, found %d", expectedRuleCount, len(rules))
	}

	for _, listMsg := range rules {
		getMsg, exists := ctl.Get(model.IngressRule.Type, listMsg.Name, listMsg.Namespace)
		if !exists {
			t.Errorf("expected IngressRule with key %v to exist", listMsg.Key())
		} else {
			listRule, ok := listMsg.Spec.(*pb.IngressRule)
			if !ok {
				t.Errorf("expected IngressRule but got %v", listMsg.Spec)
			}

			getRule, ok := getMsg.Spec.(*pb.IngressRule)
			if !ok {
				t.Errorf("expected IngressRule but got %v", getMsg)
			}

			if !reflect.DeepEqual(listRule, getRule) {
				t.Errorf("expected Get (%v) and List (%v) to return same rule", getMsg, listMsg)
			}
		}
	}

	// test edge cases for Get and List
	if _, exists := ctl.Get(model.RouteRule.Type, "test", namespace); exists {
		t.Error("Get() => got exists for route rule")
	}
	if _, exists := ctl.Get(model.IngressRule.Type, ig.Name, namespace); exists {
		t.Error("Get() => got exists for a name without a rule path")
	}
	if _, exists := ctl.Get(model.IngressRule.Type, crd.EncodeIngressRuleName("blah", 0, 0), namespace); exists {
		t.Error("Get() => got exists for a missing ingress resource")
	}
	if _, exists := ctl.Get(model.IngressRule.Type, crd.EncodeIngressRuleName(nginxIngress.Name, 0, 0), namespace); exists {
		t.Error("Get() => got exists for a different class resource")
	}
	if _, exists := ctl.Get(model.IngressRule.Type, crd.EncodeIngressRuleName(ig.Name, 10, 10), namespace); exists {
		t.Error("Get() => got exists for a unreachable rule path")
	}
	if _, err := ctl.List(model.RouteRule.Type, namespace); err == nil {
		t.Error("List() => got no error for route rules")
	}
	if elts, err := ctl.List(model.IngressRule.Type, "missing"); err != nil || len(elts) > 0 {
		t.Errorf("List() => got %#v, %v for a missing namespace", elts, err)
	}
}

func TestSyncer(t *testing.T) {
	client := fake.NewSimpleClientset()
	ip := "64.233.191.200"
	svc := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ingress",
			Namespace: namespace,
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{{
					IP: ip,
				}},
			},
		},
	}
	mesh := model.DefaultMeshConfig()
	mesh.IngressService = svc.Name

	if _, err := client.Core().Services(namespace).Create(&svc); err != nil {
		t.Error(err)
	}
	if _, err := client.Extensions().Ingresses(namespace).Create(&ig); err != nil {
		t.Error(err)
	}
	pod := "test"
	if _, err := client.Core().Pods(namespace).Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod,
			Namespace: namespace,
		},
		Spec: v1.PodSpec{
			NodeName: "node",
		},
	}); err != nil {
		t.Error(err)
	}

	// prior to invoking, need to set environment variables
	// WARNING: this pollutes environment and it's recommended to run within a bazel sandbox
	if err := os.Setenv("POD_NAME", pod); err != nil {
		t.Error(err)
	}
	if err := os.Setenv("POD_NAMESPACE", namespace); err != nil {
		t.Error(err)
	}

	stop := make(chan struct{})
	if syncer, err := crd.NewStatusSyncer(&mesh, client, namespace, kube.ControllerOptions{
		WatchedNamespace: namespace,
		ResyncPeriod:     resync,
	}); err != nil {
		t.Fatal(err)
	} else {
		go syncer.Run(stop)
	}

	count := 0
	for {
		if count > 60 {
			t.Fatalf("did not set ingress status after one minute")
		}
		<-time.After(time.Second)
		out, err := client.Extensions().Ingresses(namespace).Get(ig.Name, metav1.GetOptions{})
		if err != nil {
			continue
		}
		ips := out.Status.LoadBalancer.Ingress
		log.Infoa(ips)
		if len(ips) > 0 && ips[0].IP == ip {
			close(stop)
			break
		}
	}
}
