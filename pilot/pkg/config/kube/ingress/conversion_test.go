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
	"fmt"
	"io/ioutil"
	"sort"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/schema/collections"

	"k8s.io/api/networking/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
)

func TestGoldenConversion(t *testing.T) {
	cases := []string{"simple", "tls", "overlay"}
	for _, tt := range cases {
		t.Run(tt, func(t *testing.T) {
			input, err := readConfig(t, fmt.Sprintf("testdata/%s.yaml", tt))
			if err != nil {
				t.Fatal(err)
			}

			cfgs := map[string]*model.Config{}
			for _, obj := range input {
				ingress := obj.(*v1beta1.Ingress)
				ConvertIngressVirtualService(*ingress, "mydomain", cfgs)
			}
			ordered := []model.Config{}
			for _, v := range cfgs {
				ordered = append(ordered, *v)
			}
			for _, obj := range input {
				ingress := obj.(*v1beta1.Ingress)
				m := mesh.DefaultMeshConfig()
				gws := ConvertIngressV1alpha3(*ingress, &m, "mydomain")
				ordered = append(ordered, gws)
			}

			sort.Slice(ordered, func(i, j int) bool {
				return ordered[i].Name < ordered[j].Name
			})
			output := marshalYaml(t, ordered)
			goldenFile := fmt.Sprintf("testdata/%s.yaml.golden", tt)
			if util.Refresh() {
				if err := ioutil.WriteFile(goldenFile, output, 0644); err != nil {
					t.Fatal(err)
				}
			}
			expected, err := ioutil.ReadFile(goldenFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != string(expected) {
				t.Fatalf("expected %v, got %v", string(expected), string(output))
			}
		})
	}
}

// Print as YAML
func marshalYaml(t *testing.T, cl []model.Config) []byte {
	t.Helper()
	result := []byte{}
	separator := []byte("---\n")
	for _, config := range cl {
		s, exists := collections.All.FindByGroupVersionKind(config.GroupVersionKind)
		if !exists {
			t.Fatalf("Unknown kind %v for %v", config.GroupVersionKind, config.Name)
		}
		obj, err := crd.ConvertConfig(s, config)
		if err != nil {
			t.Fatalf("Could not decode %v: %v", config.Name, err)
		}
		bytes, err := yaml.Marshal(obj)
		if err != nil {
			t.Fatalf("Could not convert %v to YAML: %v", config, err)
		}
		result = append(result, bytes...)
		result = append(result, separator...)
	}
	return result
}

func readConfig(t *testing.T, filename string) ([]runtime.Object, error) {
	t.Helper()

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read input yaml file: %v", err)
	}
	var varr []runtime.Object
	for _, yml := range strings.Split(string(data), "\n---") {
		obj, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(yml), nil, nil)
		if err != nil {
			return nil, err
		}
		varr = append(varr, obj)
	}

	return varr, nil
}

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
	istio := mesh.DefaultMeshConfig().IngressClass
	ingressClassIstio := &v1beta1.IngressClass{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "istio",
		},
		Spec: v1beta1.IngressClassSpec{
			Controller: IstioIngressController,
		},
	}
	ingressClassOther := &v1beta1.IngressClass{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "foo",
		},
		Spec: v1beta1.IngressClassSpec{
			Controller: "foo",
		},
	}
	cases := []struct {
		annotation    string
		ingressClass  *v1beta1.IngressClass
		ingressMode   meshconfig.MeshConfig_IngressControllerMode
		shouldProcess bool
	}{
		// Annotation
		{ingressMode: meshconfig.MeshConfig_DEFAULT, annotation: "nginx", shouldProcess: false},
		{ingressMode: meshconfig.MeshConfig_STRICT, annotation: "nginx", shouldProcess: false},
		{ingressMode: meshconfig.MeshConfig_OFF, annotation: istio, shouldProcess: false},
		{ingressMode: meshconfig.MeshConfig_DEFAULT, annotation: istio, shouldProcess: true},
		{ingressMode: meshconfig.MeshConfig_STRICT, annotation: istio, shouldProcess: true},
		{ingressMode: meshconfig.MeshConfig_DEFAULT, annotation: "", shouldProcess: true},
		{ingressMode: meshconfig.MeshConfig_STRICT, annotation: "", shouldProcess: false},

		// IngressClass
		{ingressMode: meshconfig.MeshConfig_DEFAULT, ingressClass: ingressClassOther, shouldProcess: false},
		{ingressMode: meshconfig.MeshConfig_STRICT, ingressClass: ingressClassOther, shouldProcess: false},
		{ingressMode: meshconfig.MeshConfig_DEFAULT, ingressClass: ingressClassIstio, shouldProcess: true},
		{ingressMode: meshconfig.MeshConfig_STRICT, ingressClass: ingressClassIstio, shouldProcess: true},
		{ingressMode: meshconfig.MeshConfig_DEFAULT, ingressClass: nil, shouldProcess: true},
		{ingressMode: meshconfig.MeshConfig_STRICT, ingressClass: nil, shouldProcess: false},

		// IngressClass and Annotation
		{ingressMode: meshconfig.MeshConfig_STRICT, ingressClass: ingressClassIstio, annotation: "nginx", shouldProcess: false},
		{ingressMode: meshconfig.MeshConfig_STRICT, ingressClass: ingressClassOther, annotation: istio, shouldProcess: true},
		{ingressMode: -1, shouldProcess: false},
	}

	for i, c := range cases {
		className := ""
		if c.ingressClass != nil {
			className = c.ingressClass.Name
		}
		t.Run(fmt.Sprintf("%d %s %s %s", i, c.ingressMode, c.annotation, className), func(t *testing.T) {
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

			mesh := mesh.DefaultMeshConfig()
			mesh.IngressControllerMode = c.ingressMode

			if c.annotation != "" {
				ing.Annotations["kubernetes.io/ingress.class"] = c.annotation
			}

			if c.shouldProcess != shouldProcessIngressWithClass(&mesh, &ing, c.ingressClass) {
				t.Errorf("got %v, want %v",
					!c.shouldProcess, c.shouldProcess)
			}
		})
	}
}
