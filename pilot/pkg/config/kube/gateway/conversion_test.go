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
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	crdvalidation "istio.io/istio/pkg/config/crd"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestConvertResources(t *testing.T) {
	validator := crdvalidation.NewIstioValidator(t)
	cases := []struct {
		name string
	}{
		{"http"},
		{"tcp"},
		{"tls"},
		{"mismatch"},
		{"weighted"},
		{"zero"},
		{"mesh"},
		{"invalid"},
		{"multi-gateway"},
		{"delegated"},
		{"route-binding"},
		{"reference-policy-tls"},
		{"serviceentry"},
		{"eastwest"},
		{"alias"},
		{"mcs"},
		{"route-precedence"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			input := readConfig(t, fmt.Sprintf("testdata/%s.yaml", tt.name), validator)
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
					ClusterExternalAddresses: model.AddressMap{
						Addresses: map[cluster.ID][]string{
							"Kubernetes": {"1.2.3.4"},
						},
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
			output.AllowedReferences = nil       // Not tested here
			output.ReferencedNamespaceKeys = nil // Not tested here

			// sort virtual services to make the order deterministic
			sort.Slice(output.VirtualService, func(i, j int) bool {
				return output.VirtualService[i].Namespace+"/"+output.VirtualService[i].Name < output.VirtualService[j].Namespace+"/"+output.VirtualService[j].Name
			})
			goldenFile := fmt.Sprintf("testdata/%s.yaml.golden", tt.name)
			if util.Refresh() {
				res := append(output.Gateway, output.VirtualService...)
				if err := os.WriteFile(goldenFile, marshalYaml(t, res), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			golden := splitOutput(readConfig(t, goldenFile, validator))

			// sort virtual services to make the order deterministic
			sort.Slice(golden.VirtualService, func(i, j int) bool {
				return golden.VirtualService[i].Namespace+"/"+golden.VirtualService[i].Name < golden.VirtualService[j].Namespace+"/"+golden.VirtualService[j].Name
			})

			assert.Equal(t, golden, output)

			outputStatus := getStatus(t, kr.GatewayClass, kr.Gateway, kr.HTTPRoute, kr.TLSRoute, kr.TCPRoute)
			goldenStatusFile := fmt.Sprintf("testdata/%s.status.yaml.golden", tt.name)
			if util.Refresh() {
				if err := os.WriteFile(goldenStatusFile, outputStatus, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			goldenStatus, err := os.ReadFile(goldenStatusFile)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(string(goldenStatus), string(outputStatus)); diff != "" {
				t.Fatalf("Diff:\n%s", diff)
			}
		})
	}
}

func TestReferencePolicy(t *testing.T) {
	validator := crdvalidation.NewIstioValidator(t)
	type res struct {
		name, namespace string
		allowed         bool
	}
	cases := []struct {
		name         string
		config       string
		expectations []res
	}{
		{
			name: "simple",
			config: `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: ReferencePolicy
metadata:
  name: allow-gateways-to-ref-secrets
  namespace: default
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: Gateway
    namespace: istio-system
  to:
  - group: ""
    kind: Secret
`,
			expectations: []res{
				// allow cross namespace
				{"kubernetes-gateway://default/wildcard-example-com-cert", "istio-system", true},
				// denied same namespace. We do not implicitly allow (in this code - higher level code does)
				{"kubernetes-gateway://default/wildcard-example-com-cert", "default", false},
				// denied namespace
				{"kubernetes-gateway://default/wildcard-example-com-cert", "bad", false},
			},
		},
		{
			name: "multiple in one",
			config: `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: ReferencePolicy
metadata:
  name: allow-gateways-to-ref-secrets
  namespace: default
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: Gateway
    namespace: ns-1
  - group: gateway.networking.k8s.io
    kind: Gateway
    namespace: ns-2
  to:
  - group: ""
    kind: Secret
`,
			expectations: []res{
				{"kubernetes-gateway://default/wildcard-example-com-cert", "ns-1", true},
				{"kubernetes-gateway://default/wildcard-example-com-cert", "ns-2", true},
				{"kubernetes-gateway://default/wildcard-example-com-cert", "bad", false},
			},
		},
		{
			name: "multiple",
			config: `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: ReferencePolicy
metadata:
  name: ns1
  namespace: default
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: Gateway
    namespace: ns-1
  to:
  - group: ""
    kind: Secret
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: ReferencePolicy
metadata:
  name: ns2
  namespace: default
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: Gateway
    namespace: ns-2
  to:
  - group: ""
    kind: Secret
`,
			expectations: []res{
				{"kubernetes-gateway://default/wildcard-example-com-cert", "ns-1", true},
				{"kubernetes-gateway://default/wildcard-example-com-cert", "ns-2", true},
				{"kubernetes-gateway://default/wildcard-example-com-cert", "bad", false},
			},
		},
		{
			name: "same namespace",
			config: `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: ReferencePolicy
metadata:
  name: allow-gateways-to-ref-secrets
  namespace: default
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: Gateway
    namespace: default
  to:
  - group: ""
    kind: Secret
`,
			expectations: []res{
				{"kubernetes-gateway://default/wildcard-example-com-cert", "istio-system", false},
				{"kubernetes-gateway://default/wildcard-example-com-cert", "default", true},
				{"kubernetes-gateway://default/wildcard-example-com-cert", "bad", false},
			},
		},
		{
			name: "same name",
			config: `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: ReferencePolicy
metadata:
  name: allow-gateways-to-ref-secrets
  namespace: default
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: Gateway
    namespace: default
  to:
  - group: ""
    kind: Secret
    name: public
`,
			expectations: []res{
				{"kubernetes-gateway://default/public", "istio-system", false},
				{"kubernetes-gateway://default/public", "default", true},
				{"kubernetes-gateway://default/private", "default", false},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			input := readConfigString(t, tt.config, validator)
			cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{})
			kr := splitInput(input)
			kr.Context = model.NewGatewayContext(cg.PushContext())
			output := convertResources(kr)
			c := &Controller{
				state: output,
			}
			for _, sc := range tt.expectations {
				t.Run(fmt.Sprintf("%v/%v", sc.name, sc.namespace), func(t *testing.T) {
					got := c.SecretAllowed(sc.name, sc.namespace)
					if got != sc.allowed {
						t.Fatalf("expected allowed=%v, got allowed=%v", sc.allowed, got)
					}
				})
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
		Gateway:        []config.Config{},
		VirtualService: []config.Config{},
	}
	for _, c := range configs {
		c.Domain = "domain.suffix"
		switch c.GroupVersionKind {
		case gvk.Gateway:
			out.Gateway = append(out.Gateway, c)
		case gvk.VirtualService:
			out.VirtualService = append(out.VirtualService, c)
		}
	}
	return out
}

func splitInput(configs []config.Config) *KubernetesResources {
	out := &KubernetesResources{}
	namespaces := sets.New()
	for _, c := range configs {
		namespaces.Insert(c.Namespace)
		switch c.GroupVersionKind {
		case gvk.GatewayClass:
			out.GatewayClass = append(out.GatewayClass, c)
		case gvk.KubernetesGateway:
			out.Gateway = append(out.Gateway, c)
		case gvk.HTTPRoute:
			out.HTTPRoute = append(out.HTTPRoute, c)
		case gvk.TCPRoute:
			out.TCPRoute = append(out.TCPRoute, c)
		case gvk.TLSRoute:
			out.TLSRoute = append(out.TLSRoute, c)
		case gvk.ReferencePolicy:
			out.ReferencePolicy = append(out.ReferencePolicy, c)
		}
	}
	out.Namespaces = map[string]*corev1.Namespace{}
	for ns := range namespaces {
		out.Namespaces[ns] = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
				Labels: map[string]string{
					"istio.io/test-name-part": strings.Split(ns, "-")[0],
				},
			},
		}
	}
	out.Domain = "domain.suffix"
	return out
}

func readConfig(t *testing.T, filename string, validator *crdvalidation.Validator) []config.Config {
	t.Helper()

	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read input yaml file: %v", err)
	}
	return readConfigString(t, string(data), validator)
}

func readConfigString(t *testing.T, data string, validator *crdvalidation.Validator) []config.Config {
	if err := validator.ValidateCustomResourceYAML(data); err != nil {
		t.Error(err)
	}
	c, _, err := crd.ParseInputs(data)
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
		case gvk.KubernetesGateway:
			c.Status = kstatus.Wrap(&k8s.GatewayStatus{})
		case gvk.HTTPRoute:
			c.Status = kstatus.Wrap(&k8s.HTTPRouteStatus{})
		case gvk.TCPRoute:
			c.Status = kstatus.Wrap(&k8s.TCPRouteStatus{})
		case gvk.TLSRoute:
			c.Status = kstatus.Wrap(&k8s.TLSRouteStatus{})
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

func TestStrictestHost(t *testing.T) {
	tests := []struct {
		route   host.Name
		gateway host.Name
		want    string
	}{
		{"foo.com", "bar.com", ""},
		{"foo.com", "foo.com", "foo.com"},
		{"*.com", "foo.com", "foo.com"},
		{"foo.com", "*.com", "foo.com"},
		{"*.com", "*.com", "*.com"},
		{"*.foo.com", "*.bar.com", ""},
		{"*.foo.com", "*.com", ""},
		{"*", "foo.com", "foo.com"},
		{"bar.com", "", "bar.com"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v/%v", tt.route, tt.gateway), func(t *testing.T) {
			assert.Equal(t, strictestHost(tt.route, tt.gateway), tt.want)
		})
	}
}
