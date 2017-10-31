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

	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress/core/pkg/ingress/annotations/class"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/platform/kube"
	"istio.io/pilot/proxy"
)

func makeAnnotatedIngress(annotation string) *extensions.Ingress {
	if annotation == "" {
		return &extensions.Ingress{}
	}

	return &extensions.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				kube.IngressClassAnnotation: annotation,
			},
		},
	}
}

// TestConvertIngressControllerMode ensures that ingress controller mode is converted to the k8s ingress status syncer's
// representation correctly.
func TestConvertIngressControllerMode(t *testing.T) {
	cases := []struct {
		Mode       proxyconfig.MeshConfig_IngressControllerMode
		Annotation string
		Ignore     bool
	}{
		{
			Mode:       proxyconfig.MeshConfig_DEFAULT,
			Annotation: "",
			Ignore:     true,
		},
		{
			Mode:       proxyconfig.MeshConfig_DEFAULT,
			Annotation: "istio",
			Ignore:     true,
		},
		{
			Mode:       proxyconfig.MeshConfig_DEFAULT,
			Annotation: "nginx",
			Ignore:     false,
		},
		{
			Mode:       proxyconfig.MeshConfig_STRICT,
			Annotation: "",
			Ignore:     false,
		},
		{
			Mode:       proxyconfig.MeshConfig_STRICT,
			Annotation: "istio",
			Ignore:     true,
		},
		{
			Mode:       proxyconfig.MeshConfig_STRICT,
			Annotation: "nginx",
			Ignore:     false,
		},
	}

	for _, c := range cases {
		ingressClass, defaultIngressClass := convertIngressControllerMode(c.Mode, "istio")

		ing := makeAnnotatedIngress(c.Annotation)
		if ignore := class.IsValid(ing, ingressClass, defaultIngressClass); ignore != c.Ignore {
			t.Errorf("convertIngressControllerMode(%q, %q), with Ingress annotation %q => "+
				"Got ignore %v, want %v", c.Mode, "istio", c.Annotation, c.Ignore, ignore)
		}
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
	mesh := proxy.DefaultMeshConfig()
	mesh.IngressService = svc.Name

	if _, err := client.Core().Services(namespace).Create(&svc); err != nil {
		t.Error(err)
	}
	if _, err := client.Extensions().Ingresses(namespace).Create(&ingress); err != nil {
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
	if syncer, err := NewStatusSyncer(&mesh, client, namespace, kube.ControllerOptions{
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
		out, err := client.Extensions().Ingresses(namespace).Get(ingress.Name, metav1.GetOptions{})
		if err != nil {
			continue
		}
		ips := out.Status.LoadBalancer.Ingress
		glog.V(2).Info(ips)
		if len(ips) > 0 && ips[0].IP == ip {
			close(stop)
			break
		}
	}
}
