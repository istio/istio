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

package kube

import (
	"testing"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/util/intstr"
)

func TestIngressIgnored(t *testing.T) {
	cases := []struct {
		ingressClass string
		shouldIgnore bool
	}{
		{ingressClass: "nginx", shouldIgnore: true},
		{ingressClass: "istio", shouldIgnore: false},
		{ingressClass: "Istio", shouldIgnore: false},
		{ingressClass: "", shouldIgnore: false},
	}

	for _, c := range cases {
		ingress := v1beta1.Ingress{
			ObjectMeta: v1.ObjectMeta{
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

		if c.ingressClass != "" {
			ingress.Annotations["kubernetes.io/ingress.class"] = c.ingressClass
		}

		if c.shouldIgnore != ingressIgnored(&ingress) {
			t.Errorf("ingressIgnored(<ingress of class '%s'>) => %v, want %v",
				c.ingressClass, !c.shouldIgnore, c.shouldIgnore)
		}
	}
}
