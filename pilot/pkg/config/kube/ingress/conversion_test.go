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
	"testing"

	"k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

func TestConversion(t *testing.T) {
	ingress := v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Namespace: "mock", // goes into backend full name
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
		ObjectMeta: meta_v1.ObjectMeta{
			Namespace: "mock",
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
	cfgs := map[string]*model.Config{}
	ConvertIngressVirtualService(ingress, "mydomain", cfgs)
	ConvertIngressVirtualService(ingress2, "mydomain", cfgs)

	if len(cfgs) != 3 {
		t.Error("VirtualServices, expected 3 got ", len(cfgs))
	}

	for n, cfg := range cfgs {
		// Not clear if this is right - should probably be under input ns
		if cfg.ConfigMeta.Namespace != "istio-system" {
			t.Errorf("Expected istio-system namespace")
		}

		vs := cfg.Spec.(*networking.VirtualService)

		t.Log(vs)
		if n == "my.host.com" {
			if vs.Hosts[0] != "my.host.com" {
				t.Error("Unexpected host", vs)
			}
			if len(vs.Http) != 2 {
				t.Error("Unexpected rules", vs.Http)
			}
		}
	}
}

func TestDecodeIngressRuleName(t *testing.T) {
	cases := []struct {
		ingressName string
		ruleNum     int
		pathNum     int
	}{
		{"myingress", 0, 0},
		{"myingress", 1, 2},
		{"my-ingress", 1, 2},
		{"my-cool-ingress", 1, 2},
	}

	for _, c := range cases {
		encoded := EncodeIngressRuleName(c.ingressName, c.ruleNum, c.pathNum)
		ingressName, ruleNum, pathNum, err := decodeIngressRuleName(encoded)
		if err != nil {
			t.Errorf("decodeIngressRuleName(%q) => error %v", encoded, err)
		}
		if ingressName != c.ingressName || ruleNum != c.ruleNum || pathNum != c.pathNum {
			t.Errorf("decodeIngressRuleName(%q) => (%q, %d, %d), want (%q, %d, %d)",
				encoded,
				ingressName, ruleNum, pathNum,
				c.ingressName, c.ruleNum, c.pathNum,
			)
		}
	}
}

func TestEncoding(t *testing.T) {
	if got := EncodeIngressRuleName("name", 3, 5); got != "name-3-5" {
		t.Errorf("unexpected ingress encoding %q", got)
	}

	cases := []string{
		"name",
		"name-path-5",
		"name-3-path",
	}
	for _, code := range cases {
		if _, _, _, err := decodeIngressRuleName(code); err == nil {
			t.Errorf("expected error on decoding %q", code)
		}
	}
}

func TestIngressClass(t *testing.T) {
	istio := model.DefaultMeshConfig().IngressClass
	cases := []struct {
		ingressMode   meshconfig.MeshConfig_IngressControllerMode
		ingressClass  string
		shouldProcess bool
	}{
		{ingressMode: meshconfig.MeshConfig_DEFAULT, ingressClass: "nginx", shouldProcess: false},
		{ingressMode: meshconfig.MeshConfig_STRICT, ingressClass: "nginx", shouldProcess: false},
		{ingressMode: meshconfig.MeshConfig_OFF, ingressClass: istio, shouldProcess: false},
		{ingressMode: meshconfig.MeshConfig_DEFAULT, ingressClass: istio, shouldProcess: true},
		{ingressMode: meshconfig.MeshConfig_STRICT, ingressClass: istio, shouldProcess: true},
		{ingressMode: meshconfig.MeshConfig_DEFAULT, ingressClass: "", shouldProcess: true},
		{ingressMode: meshconfig.MeshConfig_STRICT, ingressClass: "", shouldProcess: false},
		{ingressMode: -1, shouldProcess: false},
	}

	for _, c := range cases {
		ing := v1beta1.Ingress{
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

		mesh := model.DefaultMeshConfig()
		mesh.IngressControllerMode = c.ingressMode

		if c.ingressClass != "" {
			ing.Annotations["kubernetes.io/ingress.class"] = c.ingressClass
		}

		if c.shouldProcess != shouldProcessIngress(&mesh, &ing) {
			t.Errorf("shouldProcessIngress(<ingress of class '%s'>) => %v, want %v",
				c.ingressClass, !c.shouldProcess, c.shouldProcess)
		}
	}
}
