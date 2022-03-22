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
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	coreV1 "k8s.io/api/core/v1"
	knetworking "k8s.io/api/networking/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/yaml"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
)

func TestGoldenConversion(t *testing.T) {
	cases := []string{"simple", "tls", "overlay", "tls-no-secret"}
	for _, tt := range cases {
		t.Run(tt, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			input, err := readConfig(t, fmt.Sprintf("testdata/%s.yaml", tt))
			if err != nil {
				t.Fatal(err)
			}
			serviceLister := createFakeLister(ctx)
			cfgs := map[string]*config.Config{}
			for _, obj := range input {
				ingress := obj.(*knetworking.Ingress)
				ConvertIngressVirtualService(*ingress, "mydomain", cfgs, serviceLister)
			}
			ordered := []config.Config{}
			for _, v := range cfgs {
				ordered = append(ordered, *v)
			}
			for _, obj := range input {
				ingress := obj.(*knetworking.Ingress)
				m := mesh.DefaultMeshConfig()
				gws := ConvertIngressV1alpha3(*ingress, m, "mydomain")
				ordered = append(ordered, gws)
			}

			sort.Slice(ordered, func(i, j int) bool {
				return ordered[i].Name < ordered[j].Name
			})
			output := marshalYaml(t, ordered)
			goldenFile := fmt.Sprintf("testdata/%s.yaml.golden", tt)
			if util.Refresh() {
				if err := os.WriteFile(goldenFile, output, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			expected, err := os.ReadFile(goldenFile)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(expected, output); diff != "" {
				t.Fatalf("Diff:\n%s", diff)
			}
		})
	}
}

// Print as YAML
func marshalYaml(t *testing.T, cl []config.Config) []byte {
	t.Helper()
	result := []byte{}
	separator := []byte("---\n")
	for _, config := range cl {
		obj, err := crd.ConvertConfig(config)
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

	data, err := os.ReadFile(filename)
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prefix := knetworking.PathTypePrefix
	exact := knetworking.PathTypeExact

	ingress := knetworking.Ingress{
		ObjectMeta: metaV1.ObjectMeta{
			Namespace: "mock", // goes into backend full name
		},
		Spec: knetworking.IngressSpec{
			Rules: []knetworking.IngressRule{
				{
					Host: "my.host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/test",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "foo",
											Port: knetworking.ServiceBackendPort{Number: 8000},
										},
									},
								},
								{
									Path:     "/test/foo",
									PathType: &prefix,
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "foo",
											Port: knetworking.ServiceBackendPort{Number: 8000},
										},
									},
								},
							},
						},
					},
				},
				{
					Host: "my2.host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/test1.*",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "bar",
											Port: knetworking.ServiceBackendPort{Number: 8000},
										},
									},
								},
							},
						},
					},
				},
				{
					Host: "my3.host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/test/*",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "bar",
											Port: knetworking.ServiceBackendPort{Number: 8000},
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
	ingress2 := knetworking.Ingress{
		ObjectMeta: metaV1.ObjectMeta{
			Namespace: "mock",
		},
		Spec: knetworking.IngressSpec{
			Rules: []knetworking.IngressRule{
				{
					Host: "my.host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/test2",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "foo",
											Port: knetworking.ServiceBackendPort{Number: 8000},
										},
									},
								},
								{
									Path:     "/test/foo/bar",
									PathType: &prefix,
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "foo",
											Port: knetworking.ServiceBackendPort{Number: 8000},
										},
									},
								},
								{
									Path:     "/test/foo/bar",
									PathType: &exact,
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "foo",
											Port: knetworking.ServiceBackendPort{Number: 8000},
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
	serviceLister := createFakeLister(ctx)
	cfgs := map[string]*config.Config{}
	ConvertIngressVirtualService(ingress, "mydomain", cfgs, serviceLister)
	ConvertIngressVirtualService(ingress2, "mydomain", cfgs, serviceLister)

	if len(cfgs) != 3 {
		t.Error("VirtualServices, expected 3 got ", len(cfgs))
	}

	expectedLength := [5]int{13, 13, 9, 6, 5}
	expectedExact := [5]bool{true, false, false, true, true}

	for n, cfg := range cfgs {
		vs := cfg.Spec.(*networking.VirtualService)

		if n == "my.host.com" {
			if vs.Hosts[0] != "my.host.com" {
				t.Error("Unexpected host", vs)
			}
			if len(vs.Http) != 5 {
				t.Error("Unexpected rules", vs.Http)
			}
			for i, route := range vs.Http {
				length, exact := getMatchURILength(route.Match[0])
				if length != expectedLength[i] || exact != expectedExact[i] {
					t.Errorf("Unexpected rule at idx:%d, want {length:%d, exact:%v}, got {length:%d, exact: %v}",
						i, expectedLength[i], expectedExact[i], length, exact)
				}
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
	ingressClassIstio := &knetworking.IngressClass{
		ObjectMeta: metaV1.ObjectMeta{
			Name: "istio",
		},
		Spec: knetworking.IngressClassSpec{
			Controller: IstioIngressController,
		},
	}
	ingressClassOther := &knetworking.IngressClass{
		ObjectMeta: metaV1.ObjectMeta{
			Name: "foo",
		},
		Spec: knetworking.IngressClassSpec{
			Controller: "foo",
		},
	}
	cases := []struct {
		annotation    string
		ingressClass  *knetworking.IngressClass
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
		// note: k8s rejects Ingress resources configured with kubernetes.io/ingress.class annotation *and* ingressClassName field so this shouldn't happen
		// see https://github.com/kubernetes/kubernetes/blob/ededd08ba131b727e60f663bd7217fffaaccd448/pkg/apis/networking/validation/validation.go#L226
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
			ing := knetworking.Ingress{
				ObjectMeta: metaV1.ObjectMeta{
					Name:        "test-ingress",
					Namespace:   "default",
					Annotations: make(map[string]string),
				},
				Spec: knetworking.IngressSpec{
					DefaultBackend: &knetworking.IngressBackend{
						Service: &knetworking.IngressServiceBackend{
							Name: "default-http-backend",
							Port: knetworking.ServiceBackendPort{Number: 8000},
						},
					},
				},
			}

			mesh := mesh.DefaultMeshConfig()
			mesh.IngressControllerMode = c.ingressMode

			if c.annotation != "" {
				ing.Annotations["kubernetes.io/ingress.class"] = c.annotation
			}

			if c.shouldProcess != shouldProcessIngressWithClass(mesh, &ing, c.ingressClass) {
				t.Errorf("got %v, want %v",
					!c.shouldProcess, c.shouldProcess)
			}
		})
	}
}

func TestNamedPortIngressConversion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ingress := knetworking.Ingress{
		ObjectMeta: metaV1.ObjectMeta{
			Namespace: "mock",
		},
		Spec: knetworking.IngressSpec{
			Rules: []knetworking.IngressRule{
				{
					Host: "host.com",
					IngressRuleValue: knetworking.IngressRuleValue{
						HTTP: &knetworking.HTTPIngressRuleValue{
							Paths: []knetworking.HTTPIngressPath{
								{
									Path: "/test/*",
									Backend: knetworking.IngressBackend{
										Service: &knetworking.IngressServiceBackend{
											Name: "foo",
											Port: knetworking.ServiceBackendPort{Name: "test-svc-port"},
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
	service := &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "foo",
			Namespace: "mock",
		},
		Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{
				{
					Name:     "test-svc-port",
					Protocol: "TCP",
					Port:     8888,
					TargetPort: intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "test-port",
					},
				},
			},
			Selector: map[string]string{
				"app": "test-app",
			},
		},
	}
	serviceLister := createFakeLister(ctx, service)
	cfgs := map[string]*config.Config{}
	ConvertIngressVirtualService(ingress, "mydomain", cfgs, serviceLister)
	if len(cfgs) != 1 {
		t.Error("VirtualServices, expected 1 got ", len(cfgs))
	}
	if cfgs["host.com"] == nil {
		t.Error("Host, found nil")
	}
	vs := cfgs["host.com"].Spec.(*networking.VirtualService)
	if len(vs.Http) != 1 {
		t.Error("HttpSpec, expected 1 got ", len(vs.Http))
	}
	http := vs.Http[0]
	if len(http.Route) != 1 {
		t.Error("Route, expected 1 got ", len(http.Route))
	}
	route := http.Route[0]
	if route.Destination.Port.Number != 8888 {
		t.Error("PortNumer, expected 8888 got ", route.Destination.Port.Number)
	}
}

func createFakeLister(ctx context.Context, objects ...runtime.Object) listerv1.ServiceLister {
	client := fake.NewSimpleClientset(objects...)
	informerFactory := informers.NewSharedInformerFactory(client, time.Hour)
	svcInformer := informerFactory.Core().V1().Services().Informer()
	go svcInformer.Run(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), svcInformer.HasSynced)
	return informerFactory.Core().V1().Services().Lister()
}
