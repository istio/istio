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

package ingress

import (
	"os"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"

	crd "istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/test"
)

const (
	namespace = "test"
	resync    = 1 * time.Second
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

func TestSyncer(t *testing.T) {
	if !tc.Ingress || tc.V1alpha3 {
		t.Skipf("Skipping %s: ingress=false", t.Name())
	}
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
	defer close(stop)
	opts := kube.ControllerOptions{
		WatchedNamespace: namespace,
		ResyncPeriod:     resync,
	}
	if syncer, err := crd.NewStatusSyncer(&mesh, client, namespace, opts); err != nil {
		t.Fatal(err)
	} else {
		go syncer.Run(stop)
	}

	test.Eventually(t, "set ingress status with correct ip", func() bool {
		out, err := client.Extensions().Ingresses(namespace).Get(ig.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to load ingress with err: %v", err)
		}
		return len(out.Status.LoadBalancer.Ingress) > 0 && out.Status.LoadBalancer.Ingress[0].IP != ip
	})
}
