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

package gateway

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	crdvalidation "istio.io/istio/pkg/config/crd"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
)

func TestConvertResources(t *testing.T) {
	validator := crdvalidation.NewIstioValidator(t)
	cases := []string{
		"http",
		"tcp",
		"tls",
		"mismatch",
		"weighted",
		"backendpolicy",
		"mesh",
		"invalid",
		"multi-gateway",
		"delegated",
	}
	for _, tt := range cases {
		t.Run(tt, func(t *testing.T) {
			input := readConfig(t, fmt.Sprintf("testdata/%s.yaml", tt), validator)
			// Setup a few preconfigured services
			ports := []*model.Port{
				{
					Name:     "http",
					Port:     80,
					Protocol: "HTTP",
				},
				{
					Name:     "tcp",
					Port:     34000,
					Protocol: "TCP",
				},
			}
			ingressSvc := &model.Service{
				Attributes: model.ServiceAttributes{
					Name:      "istio-ingressgateway",
					Namespace: "istio-system",
					ClusterExternalAddresses: map[cluster.ID][]string{
						"Kubernetes": {"1.2.3.4"},
					},
				},
				Ports:    ports,
				Hostname: "istio-ingressgateway.istio-system.svc.domain.suffix",
			}
			altIngressSvc := &model.Service{
				Attributes: model.ServiceAttributes{
					Namespace: "istio-system",
				},
				Ports:    ports,
				Hostname: "example.com",
			}
			cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
				Services: []*model.Service{ingressSvc, altIngressSvc},
				Instances: []*model.ServiceInstance{
					{Service: ingressSvc, ServicePort: ingressSvc.Ports[0], Endpoint: &model.IstioEndpoint{EndpointPort: 8080}},
					{Service: ingressSvc, ServicePort: ingressSvc.Ports[1], Endpoint: &model.IstioEndpoint{}},
					{Service: altIngressSvc, ServicePort: altIngressSvc.Ports[0], Endpoint: &model.IstioEndpoint{}},
					{Service: altIngressSvc, ServicePort: altIngressSvc.Ports[1], Endpoint: &model.IstioEndpoint{}},
				},
			},
			)
			kr := splitInput(input)
			kr.Context = model.NewGatewayContext(cg.PushContext())
			output := convertResources(kr)

			goldenFile := fmt.Sprintf("testdata/%s.yaml.golden", tt)
			if util.Refresh() {
				res := append(output.Gateway, output.VirtualService...)
				res = append(res, output.DestinationRule...)
				if err := ioutil.WriteFile(goldenFile, marshalYaml(t, res), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			golden := splitOutput(readConfig(t, goldenFile, validator))
			if diff := cmp.Diff(golden, output); diff != "" {
				t.Fatalf("Diff:\n%s", diff)
			}

			outputStatus := getStatus(t, kr.GatewayClass, kr.Gateway, kr.HTTPRoute, kr.TLSRoute, kr.TCPRoute, kr.BackendPolicy)
			goldenStatusFile := fmt.Sprintf("testdata/%s.status.yaml.golden", tt)
			if util.Refresh() {
				if err := ioutil.WriteFile(goldenStatusFile, outputStatus, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			goldenStatus, err := ioutil.ReadFile(goldenStatusFile)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(string(goldenStatus), string(outputStatus)); diff != "" {
				t.Fatalf("Diff:\n%s", diff)
			}
		})
	}
}

func getStatus(t test.Failer, acfgs ...[]config.Config) []byte {
	cfgs := []config.Config{}
	for _, cl := range acfgs {
		cfgs = append(cfgs, cl...)
	}
	for i, c := range cfgs {
		c = c.DeepCopy()
		c.Spec = nil
		c.Labels = nil
		c.Annotations = nil
		if c.Status.(*kstatus.WrappedStatus) != nil {
			c.Status = c.Status.(*kstatus.WrappedStatus).Status
		}
		cfgs[i] = c
	}
	return timestampRegex.ReplaceAll(marshalYaml(t, cfgs), []byte("lastTransitionTime: fake"))
}

var timestampRegex = regexp.MustCompile(`lastTransitionTime:.*`)

func splitOutput(configs []config.Config) OutputResources {
	out := OutputResources{
		Gateway:         []config.Config{},
		VirtualService:  []config.Config{},
		DestinationRule: []config.Config{},
	}
	for _, c := range configs {
		c.Domain = "domain.suffix"
		switch c.GroupVersionKind {
		case gvk.Gateway:
			out.Gateway = append(out.Gateway, c)
		case gvk.VirtualService:
			out.VirtualService = append(out.VirtualService, c)
		case gvk.DestinationRule:
			out.DestinationRule = append(out.DestinationRule, c)
		}
	}
	return out
}

func splitInput(configs []config.Config) *KubernetesResources {
	out := &KubernetesResources{}
	namespaces := sets.NewSet()
	for _, c := range configs {
		namespaces.Insert(c.Namespace)
		switch c.GroupVersionKind {
		case gvk.GatewayClass:
			out.GatewayClass = append(out.GatewayClass, c)
		case gvk.ServiceApisGateway:
			out.Gateway = append(out.Gateway, c)
		case gvk.HTTPRoute:
			out.HTTPRoute = append(out.HTTPRoute, c)
		case gvk.TCPRoute:
			out.TCPRoute = append(out.TCPRoute, c)
		case gvk.TLSRoute:
			out.TLSRoute = append(out.TLSRoute, c)
		case gvk.BackendPolicy:
			out.BackendPolicy = append(out.BackendPolicy, c)
		}
	}
	out.Namespaces = map[string]*corev1.Namespace{}
	for ns := range namespaces {
		out.Namespaces[ns] = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}
	}
	out.Domain = "domain.suffix"
	return out
}

func readConfig(t *testing.T, filename string, validator *crdvalidation.Validator) []config.Config {
	t.Helper()

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read input yaml file: %v", err)
	}
	if err := validator.ValidateCustomResourceYAML(string(data)); err != nil {
		t.Error(err)
	}
	c, _, err := crd.ParseInputs(string(data))
	if err != nil {
		t.Fatalf("failed to parse CRD: %v", err)
	}
	return insertDefaults(c)
}

// insertDefaults sets default values that would be present when reading from Kubernetes but not from
// files
func insertDefaults(cfgs []config.Config) []config.Config {
	res := make([]config.Config, 0, len(cfgs))
	for _, c := range cfgs {
		switch c.GroupVersionKind {
		case gvk.GatewayClass:
			c.Status = kstatus.Wrap(&k8s.GatewayClassStatus{})
		case gvk.ServiceApisGateway:
			c.Status = kstatus.Wrap(&k8s.GatewayStatus{})
		case gvk.HTTPRoute:
			c.Status = kstatus.Wrap(&k8s.HTTPRouteStatus{})
		case gvk.TCPRoute:
			c.Status = kstatus.Wrap(&k8s.TCPRouteStatus{})
		case gvk.TLSRoute:
			c.Status = kstatus.Wrap(&k8s.TLSRouteStatus{})
		case gvk.BackendPolicy:
			c.Status = kstatus.Wrap(&k8s.BackendPolicyStatus{})
		}
		res = append(res, c)
	}
	return res
}

// Print as YAML
func marshalYaml(t test.Failer, cl []config.Config) []byte {
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

func TestStandardizeWeight(t *testing.T) {
	tests := []struct {
		name   string
		input  []int
		output []int
	}{
		{"single", []int{1}, []int{0}},
		{"double", []int{1, 1}, []int{50, 50}},
		{"zero", []int{1, 0}, []int{100, 0}},
		{"all zero", []int{0, 0}, []int{50, 50}},
		{"overflow", []int{1, 1, 1}, []int{34, 33, 33}},
		{"skewed", []int{9, 1}, []int{90, 10}},
		{"multiple overflow", []int{1, 1, 1, 1, 1, 1}, []int{17, 17, 17, 17, 16, 16}},
		{"skewed overflow", []int{1, 1, 1, 3}, []int{17, 17, 16, 50}},
		{"skewed overflow 2", []int{1, 1, 1, 1, 2}, []int{17, 17, 17, 16, 33}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := standardizeWeights(tt.input)
			if !reflect.DeepEqual(tt.output, got) {
				t.Errorf("standardizeWeights() = %v, want %v", got, tt.output)
			}
			if len(tt.output) > 1 && intSum(tt.output) != 100 {
				t.Errorf("invalid weights, should sum to 100: %v", got)
			}
		})
	}
}

func TestIsRouteMatch(t *testing.T) {
	namespaces := map[string]*corev1.Namespace{
		"select": {ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"selected": "true"},
		}},
		"not-select": {ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"selected": "false"},
		}},
	}
	cases := []struct {
		name     string
		cfg      config.Config
		gateway  config.Meta
		routes   k8s.RouteBindingSelector
		expected bool
	}{
		{
			name: "defaults, same namespace",
			cfg: config.Config{
				Meta: config.Meta{Namespace: "default", GroupVersionKind: gvk.HTTPRoute},
			},
			gateway: config.Meta{Name: "gateway", Namespace: "default"},
			routes: k8s.RouteBindingSelector{
				Kind:  gvk.HTTPRoute.Kind,
				Group: StrPointer(gvk.HTTPRoute.Group),
			},
			// Default for both selectors will match everything in the same namespace
			expected: true,
		},
		{
			name: "defaults, different namespace",
			cfg: config.Config{
				Meta: config.Meta{Namespace: "default", GroupVersionKind: gvk.HTTPRoute},
			},
			gateway: config.Meta{Name: "gateway", Namespace: "not-default"},
			routes: k8s.RouteBindingSelector{
				Kind:  gvk.HTTPRoute.Kind,
				Group: StrPointer(gvk.HTTPRoute.Group),
			},
			// Default for both selectors will match everything in the same namespace
			expected: false,
		},
		{
			name: "route all, gateway all",
			cfg: config.Config{
				Meta: config.Meta{Namespace: "default", GroupVersionKind: gvk.HTTPRoute},
				Spec: &k8s.HTTPRouteSpec{
					Gateways: &k8s.RouteGateways{Allow: &gatewayAll},
				},
			},
			gateway: config.Meta{Name: "gateway", Namespace: "not-default"},
			routes: k8s.RouteBindingSelector{
				Namespaces: &k8s.RouteNamespaces{From: &routeAll},
				Group:      StrPointer(gvk.HTTPRoute.Group),
				Kind:       gvk.HTTPRoute.Kind,
			},
			// Both allow cross namespace, this is allowed
			expected: true,
		},
		{
			name: "route all, gateway same",
			cfg: config.Config{
				Meta: config.Meta{Namespace: "default", GroupVersionKind: gvk.HTTPRoute},
				Spec: &k8s.HTTPRouteSpec{
					Gateways: &k8s.RouteGateways{Allow: &gatewayAll},
				},
			},
			gateway: config.Meta{Name: "gateway", Namespace: "not-default"},
			routes: k8s.RouteBindingSelector{
				Group: StrPointer(gvk.HTTPRoute.Group),
				Kind:  gvk.HTTPRoute.Kind,
			},
			// Gateway isn't looking in other namespaces
			expected: false,
		},
		{
			name: "route same, gateway all",
			cfg: config.Config{
				Meta: config.Meta{Namespace: "default", GroupVersionKind: gvk.HTTPRoute},
			},
			gateway: config.Meta{Name: "gateway", Namespace: "not-default"},
			routes: k8s.RouteBindingSelector{
				Namespaces: &k8s.RouteNamespaces{From: &routeAll},
				Group:      StrPointer(gvk.HTTPRoute.Group),
				Kind:       gvk.HTTPRoute.Kind,
			},
			// Route doesn't allow cross namespace
			expected: false,
		},
		{
			name: "route references match",
			cfg: config.Config{
				Meta: config.Meta{Namespace: "default", GroupVersionKind: gvk.HTTPRoute},
				Spec: &k8s.HTTPRouteSpec{
					Gateways: &k8s.RouteGateways{Allow: &gatewayAll, GatewayRefs: []k8s.GatewayReference{
						{Name: "gateway", Namespace: "not-default"},
					}},
				},
			},
			gateway: config.Meta{Name: "gateway", Namespace: "not-default"},
			routes: k8s.RouteBindingSelector{
				Namespaces: &k8s.RouteNamespaces{From: &routeAll},
				Group:      StrPointer(gvk.HTTPRoute.Group),
				Kind:       gvk.HTTPRoute.Kind,
			},
			// direct reference matches
			expected: true,
		},
		{
			name: "route references no match",
			cfg: config.Config{
				Meta: config.Meta{Namespace: "default", GroupVersionKind: gvk.HTTPRoute},
				Spec: &k8s.HTTPRouteSpec{
					Gateways: &k8s.RouteGateways{
						Allow: func() *k8s.GatewayAllowType {
							x := k8s.GatewayAllowFromList
							return &x
						}(),
						GatewayRefs: []k8s.GatewayReference{
							{Name: "not-gateway", Namespace: "not-default"},
						},
					},
				},
			},
			gateway: config.Meta{Name: "gateway", Namespace: "not-default"},
			routes: k8s.RouteBindingSelector{
				Namespaces: &k8s.RouteNamespaces{From: &routeAll},
				Group:      StrPointer(gvk.HTTPRoute.Group),
				Kind:       gvk.HTTPRoute.Kind,
			},
			// direct reference does not match
			expected: false,
		},
		{
			name: "gateway selector matches",
			cfg: config.Config{
				Meta: config.Meta{Namespace: "select", GroupVersionKind: gvk.HTTPRoute},
				Spec: &k8s.HTTPRouteSpec{
					Gateways: &k8s.RouteGateways{Allow: &routeAllow},
				},
			},
			gateway: config.Meta{Name: "gateway", Namespace: "not-default"},
			routes: k8s.RouteBindingSelector{
				Namespaces: &k8s.RouteNamespaces{
					From: func() *k8s.RouteSelectType {
						x := k8s.RouteSelectSelector
						return &x
					}(),
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
						"selected": "true",
					}},
				},
				Group: StrPointer(gvk.HTTPRoute.Group),
				Kind:  gvk.HTTPRoute.Kind,
			},
			// selector matches namespace label
			expected: true,
		},
		{
			name: "gateway selector no match",
			cfg: config.Config{
				Meta: config.Meta{Namespace: "not-select", GroupVersionKind: gvk.HTTPRoute},
				Spec: &k8s.HTTPRouteSpec{
					Gateways: &k8s.RouteGateways{Allow: &gatewayAll},
				},
			},
			gateway: config.Meta{Name: "gateway", Namespace: "not-default"},
			routes: k8s.RouteBindingSelector{
				Namespaces: &k8s.RouteNamespaces{
					From: func() *k8s.RouteSelectType {
						x := k8s.RouteSelectSelector
						return &x
					}(),
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
						"selected": "true",
					}},
				},
				Group: StrPointer(gvk.HTTPRoute.Group),
				Kind:  gvk.HTTPRoute.Kind,
			},
			// selector does not match namespace
			expected: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := isRouteMatch(tt.cfg, tt.gateway, tt.routes, namespaces)
			if got != tt.expected {
				t.Fatalf("expected match=%v, got match=%v", tt.expected, got)
			}
		})
	}
}

func TestHumanReadableJoin(t *testing.T) {
	tests := []struct {
		input []string
		want  string
	}{
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a and b"},
		{[]string{"a", "b", "c"}, "a, b, and c"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.input, "_"), func(t *testing.T) {
			if got := humanReadableJoin(tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
