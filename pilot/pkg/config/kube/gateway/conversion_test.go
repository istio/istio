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
	"k8s.io/apimachinery/pkg/runtime"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"
	"sigs.k8s.io/yaml"

	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	credentials "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	crdvalidation "istio.io/istio/pkg/config/crd"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

var ports = []*model.Port{
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
	{
		Name:     "tcp-other",
		Port:     34001,
		Protocol: "TCP",
	},
}

var services = []*model.Service{
	{
		Attributes: model.ServiceAttributes{
			Name:      "istio-ingressgateway",
			Namespace: "istio-system",
			ClusterExternalAddresses: &model.AddressMap{
				Addresses: map[cluster.ID][]string{
					constants.DefaultClusterName: {"1.2.3.4"},
				},
			},
		},
		Ports:    ports,
		Hostname: "istio-ingressgateway.istio-system.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "istio-system",
		},
		Ports:    ports,
		Hostname: "example.com",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "httpbin.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "apple",
		},
		Ports:    ports,
		Hostname: "httpbin-apple.apple.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "banana",
		},
		Ports:    ports,
		Hostname: "httpbin-banana.banana.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "httpbin-second.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "httpbin-wildcard.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "foo-svc.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "httpbin-other.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "example.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "echo.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "echo.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "cert",
		},
		Ports:    ports,
		Hostname: "httpbin.cert.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "service",
		},
		Ports:    ports,
		Hostname: "my-svc.service.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "google.com",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "allowed-1",
		},
		Ports:    ports,
		Hostname: "svc2.allowed-1.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "allowed-2",
		},
		Ports:    ports,
		Hostname: "svc2.allowed-2.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "allowed-1",
		},
		Ports:    ports,
		Hostname: "svc1.allowed-1.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "allowed-2",
		},
		Ports:    ports,
		Hostname: "svc3.allowed-2.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "svc4.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "group-namespace1",
		},
		Ports:    ports,
		Hostname: "httpbin.group-namespace1.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "group-namespace2",
		},
		Ports:    ports,
		Hostname: "httpbin.group-namespace2.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "httpbin-zero.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "istio-system",
		},
		Ports:    ports,
		Hostname: "httpbin.istio-system.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "httpbin-mirror.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "httpbin-foo.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "httpbin-alt.default.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "istio-system",
		},
		Ports:    ports,
		Hostname: "istiod.istio-system.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "istio-system",
		},
		Ports:    ports,
		Hostname: "istiod.istio-system.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "istio-system",
		},
		Ports:    ports,
		Hostname: "echo.istio-system.svc.domain.suffix",
	},
	{
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
		Ports:    ports,
		Hostname: "httpbin-bad.default.svc.domain.suffix",
	},
}

var (
	// https://github.com/kubernetes/kubernetes/blob/v1.25.4/staging/src/k8s.io/kubectl/pkg/cmd/create/create_secret_tls_test.go#L31
	rsaCertPEM = `-----BEGIN CERTIFICATE-----
MIIB0zCCAX2gAwIBAgIJAI/M7BYjwB+uMA0GCSqGSIb3DQEBBQUAMEUxCzAJBgNV
BAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJbnRlcm5ldCBX
aWRnaXRzIFB0eSBMdGQwHhcNMTIwOTEyMjE1MjAyWhcNMTUwOTEyMjE1MjAyWjBF
MQswCQYDVQQGEwJBVTETMBEGA1UECAwKU29tZS1TdGF0ZTEhMB8GA1UECgwYSW50
ZXJuZXQgV2lkZ2l0cyBQdHkgTHRkMFwwDQYJKoZIhvcNAQEBBQADSwAwSAJBANLJ
hPHhITqQbPklG3ibCVxwGMRfp/v4XqhfdQHdcVfHap6NQ5Wok/4xIA+ui35/MmNa
rtNuC+BdZ1tMuVCPFZcCAwEAAaNQME4wHQYDVR0OBBYEFJvKs8RfJaXTH08W+SGv
zQyKn0H8MB8GA1UdIwQYMBaAFJvKs8RfJaXTH08W+SGvzQyKn0H8MAwGA1UdEwQF
MAMBAf8wDQYJKoZIhvcNAQEFBQADQQBJlffJHybjDGxRMqaRmDhX0+6v02TUKZsW
r5QuVbpQhH6u+0UgcW0jp9QwpxoPTLTWGXEWBBBurxFwiCBhkQ+V
-----END CERTIFICATE-----
`
	rsaKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIBOwIBAAJBANLJhPHhITqQbPklG3ibCVxwGMRfp/v4XqhfdQHdcVfHap6NQ5Wo
k/4xIA+ui35/MmNartNuC+BdZ1tMuVCPFZcCAwEAAQJAEJ2N+zsR0Xn8/Q6twa4G
6OB1M1WO+k+ztnX/1SvNeWu8D6GImtupLTYgjZcHufykj09jiHmjHx8u8ZZB/o1N
MQIhAPW+eyZo7ay3lMz1V01WVjNKK9QSn1MJlb06h/LuYv9FAiEA25WPedKgVyCW
SmUwbPw8fnTcpqDWE3yTO3vKcebqMSsCIBF3UmVue8YU3jybC3NxuXq3wNm34R8T
xVLHwDXh/6NJAiEAl2oHGGLz64BuAfjKrqwz7qMYr9HCLIe/YsoWq/olzScCIQDi
D2lWusoe2/nEqfDVVWGWlyJ7yOmqaVm/iNUN9B2N2g==
-----END RSA PRIVATE KEY-----
`

	secrets = []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-cert-http",
				Namespace: "istio-system",
			},
			Data: map[string][]byte{
				"tls.crt": []byte(rsaCertPEM),
				"tls.key": []byte(rsaKeyPEM),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cert",
				Namespace: "cert",
			},
			Data: map[string][]byte{
				"tls.crt": []byte(rsaCertPEM),
				"tls.key": []byte(rsaKeyPEM),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "malformed",
				Namespace: "istio-system",
			},
			Data: map[string][]byte{
				// nolint: lll
				// https://github.com/kubernetes-sigs/gateway-api/blob/d7f71d6b7df7e929ae299948973a693980afc183/conformance/tests/gateway-invalid-tls-certificateref.yaml#L87-L90
				// this certificate is invalid because contains an invalid pem (base64 of "Hello world"),
				// and the certificate and the key are identical
				"tls.crt": []byte("SGVsbG8gd29ybGQK"),
				"tls.key": []byte("SGVsbG8gd29ybGQK"),
			},
		},
	}
)

func init() {
	features.EnableAlphaGatewayAPI = true
	features.EnableAmbientWaypoints = true
	// Recompute with ambient enabled
	classInfos = getClassInfos()
	builtinClasses = getBuiltinClasses()
}

func TestConvertResources(t *testing.T) {
	validator := crdvalidation.NewIstioValidator(t)
	cases := []struct {
		name string
		// Some configs are intended to be generated with invalid configs, and since they will be validated
		// by the validator, we need to ignore the validation errors to prevent the test from failing.
		validationIgnorer *crdvalidation.ValidationIgnorer
	}{
		{name: "http"},
		{name: "tcp"},
		{name: "tls"},
		{name: "grpc"},
		{name: "mismatch"},
		{name: "weighted"},
		{name: "zero"},
		{name: "mesh"},
		{
			name: "invalid",
			validationIgnorer: crdvalidation.NewValidationIgnorer(
				"default/^invalid-backendRef-kind-",
				"default/^invalid-backendRef-mixed-",
			),
		},
		{name: "multi-gateway"},
		{name: "delegated"},
		{name: "route-binding"},
		{name: "reference-policy-tls"},
		{
			name: "reference-policy-service",
			validationIgnorer: crdvalidation.NewValidationIgnorer(
				"istio-system/^backend-not-allowed-",
			),
		},
		{
			name: "reference-policy-tcp",
			validationIgnorer: crdvalidation.NewValidationIgnorer(
				"istio-system/^not-allowed-echo-",
			),
		},
		{name: "serviceentry"},
		{name: "eastwest"},
		{name: "eastwest-tlsoption"},
		{name: "eastwest-labelport"},
		{name: "eastwest-remote"},
		{name: "alias"},
		{name: "mcs"},
		{name: "route-precedence"},
		{name: "waypoint"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			input := readConfig(t, fmt.Sprintf("testdata/%s.yaml", tt.name), validator, nil)
			// Setup a few preconfigured services
			instances := []*model.ServiceInstance{}
			for _, svc := range services {
				instances = append(instances, &model.ServiceInstance{
					Service:     svc,
					ServicePort: ports[0],
					Endpoint:    &model.IstioEndpoint{EndpointPort: 8080},
				}, &model.ServiceInstance{
					Service:     svc,
					ServicePort: ports[1],
					Endpoint:    &model.IstioEndpoint{},
				}, &model.ServiceInstance{
					Service:     svc,
					ServicePort: ports[2],
					Endpoint:    &model.IstioEndpoint{},
				})
			}
			cg := core.NewConfigGenTest(t, core.TestOptions{
				Services:  services,
				Instances: instances,
			})
			kr := splitInput(t, input)
			kr.Context = NewGatewayContext(cg.PushContext(), "Kubernetes")
			output := convertResources(kr)
			output.AllowedReferences = AllowedReferences{} // Not tested here
			output.ReferencedNamespaceKeys = nil           // Not tested here
			output.ResourceReferences = nil                // Not tested here

			// sort virtual services to make the order deterministic
			sort.Slice(output.VirtualService, func(i, j int) bool {
				return output.VirtualService[i].Namespace+"/"+output.VirtualService[i].Name < output.VirtualService[j].Namespace+"/"+output.VirtualService[j].Name
			})
			goldenFile := fmt.Sprintf("testdata/%s.yaml.golden", tt.name)
			res := append(output.Gateway, output.VirtualService...)
			util.CompareContent(t, marshalYaml(t, res), goldenFile)
			golden := splitOutput(readConfig(t, goldenFile, validator, tt.validationIgnorer))

			// sort virtual services to make the order deterministic
			sort.Slice(golden.VirtualService, func(i, j int) bool {
				return golden.VirtualService[i].Namespace+"/"+golden.VirtualService[i].Name < golden.VirtualService[j].Namespace+"/"+golden.VirtualService[j].Name
			})

			assert.Equal(t, golden, output)

			outputStatus := getStatus(t, kr.GatewayClass, kr.Gateway, kr.HTTPRoute, kr.GRPCRoute, kr.TLSRoute, kr.TCPRoute)
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

func TestSortHTTPRoutes(t *testing.T) {
	cases := []struct {
		name string
		in   []*istio.HTTPRoute
		out  []*istio.HTTPRoute
	}{
		{
			"match is preferred over no match",
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Exact{
									Exact: "/foo",
								},
							},
						},
					},
				},
			},
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Exact{
									Exact: "/foo",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{},
				},
			},
		},
		{
			"path matching exact > prefix  > regex",
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Regex{
									Regex: ".*foo",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Exact{
									Exact: "/foo",
								},
							},
						},
					},
				},
			},
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Exact{
									Exact: "/foo",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Regex{
									Regex: ".*foo",
								},
							},
						},
					},
				},
			},
		},
		{
			"path prefix matching with largest characters",
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/foo",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/foobar",
								},
							},
						},
					},
				},
			},
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/foobar",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/foo",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/",
								},
							},
						},
					},
				},
			},
		},
		{
			"path match is preferred over method match",
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Method: &istio.StringMatch{
								MatchType: &istio.StringMatch_Exact{
									Exact: "GET",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/foobar",
								},
							},
						},
					},
				},
			},
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/foobar",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Method: &istio.StringMatch{
								MatchType: &istio.StringMatch_Exact{
									Exact: "GET",
								},
							},
						},
					},
				},
			},
		},
		{
			"largest number of header matches is preferred",
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Headers: map[string]*istio.StringMatch{
								"header1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Headers: map[string]*istio.StringMatch{
								"header1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
								"header2": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value2",
									},
								},
							},
						},
					},
				},
			},
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Headers: map[string]*istio.StringMatch{
								"header1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
								"header2": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value2",
									},
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Headers: map[string]*istio.StringMatch{
								"header1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			"largest number of query params is preferred",
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							QueryParams: map[string]*istio.StringMatch{
								"param1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							QueryParams: map[string]*istio.StringMatch{
								"param1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
								"param2": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value2",
									},
								},
							},
						},
					},
				},
			},
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							QueryParams: map[string]*istio.StringMatch{
								"param1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
								"param2": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value2",
									},
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							QueryParams: map[string]*istio.StringMatch{
								"param1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			"path > method > header > query params",
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							QueryParams: map[string]*istio.StringMatch{
								"param1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Method: &istio.StringMatch{
								MatchType: &istio.StringMatch_Exact{Exact: "GET"},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Headers: map[string]*istio.StringMatch{
								"param1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
							},
						},
					},
				},
			},
			[]*istio.HTTPRoute{
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Uri: &istio.StringMatch{
								MatchType: &istio.StringMatch_Prefix{
									Prefix: "/",
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Method: &istio.StringMatch{
								MatchType: &istio.StringMatch_Exact{Exact: "GET"},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							Headers: map[string]*istio.StringMatch{
								"param1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
							},
						},
					},
				},
				{
					Match: []*istio.HTTPMatchRequest{
						{
							QueryParams: map[string]*istio.StringMatch{
								"param1": {
									MatchType: &istio.StringMatch_Exact{
										Exact: "value1",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sortHTTPRoutes(tt.in)
			if !reflect.DeepEqual(tt.in, tt.out) {
				t.Fatalf("expected %v, got %v", tt.out, tt.in)
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
			config: `apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
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
			config: `apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
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
			config: `apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
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
apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
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
			config: `apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
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
			config: `apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
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
			input := readConfigString(t, tt.config, validator, nil)
			cg := core.NewConfigGenTest(t, core.TestOptions{})
			kr := splitInput(t, input)
			kr.Context = NewGatewayContext(cg.PushContext(), "Kubernetes")
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
		if c.Status.(*kstatus.WrappedStatus) != nil && c.GroupVersionKind == gvk.GatewayClass {
			// Override GatewaySupportedFeatures for the test so we dont have huge golden files plus we wont need to update them every time we support a new feature
			c.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
				gcs := s.(*k8sv1.GatewayClassStatus)
				gcs.SupportedFeatures = []k8sv1.SupportedFeature{"HTTPRouteFeatureA", "HTTPRouteFeatureB"}
				return gcs
			})
		}
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

func splitOutput(configs []config.Config) IstioResources {
	out := IstioResources{
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

func splitInput(t test.Failer, configs []config.Config) GatewayResources {
	out := GatewayResources{}
	namespaces := sets.New[string]()
	for _, c := range configs {
		namespaces.Insert(c.Namespace)
		switch c.GroupVersionKind {
		case gvk.GatewayClass:
			out.GatewayClass = append(out.GatewayClass, c)
		case gvk.KubernetesGateway:
			out.Gateway = append(out.Gateway, c)
		case gvk.HTTPRoute:
			out.HTTPRoute = append(out.HTTPRoute, c)
		case gvk.GRPCRoute:
			out.GRPCRoute = append(out.GRPCRoute, c)
		case gvk.TCPRoute:
			out.TCPRoute = append(out.TCPRoute, c)
		case gvk.TLSRoute:
			out.TLSRoute = append(out.TLSRoute, c)
		case gvk.ReferenceGrant:
			out.ReferenceGrant = append(out.ReferenceGrant, c)
		case gvk.ServiceEntry:
			out.ServiceEntry = append(out.ServiceEntry, c)
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

	client := kube.NewFakeClient(secrets...)
	out.Credentials = credentials.NewCredentialsController(client, nil)
	client.RunAndWait(test.NewStop(t))

	out.Domain = "domain.suffix"
	return out
}

func readConfig(t testing.TB, filename string, validator *crdvalidation.Validator, ignorer *crdvalidation.ValidationIgnorer) []config.Config {
	t.Helper()

	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read input yaml file: %v", err)
	}
	return readConfigString(t, string(data), validator, ignorer)
}

func readConfigString(t testing.TB, data string, validator *crdvalidation.Validator, ignorer *crdvalidation.ValidationIgnorer,
) []config.Config {
	if err := validator.ValidateCustomResourceYAML(data, ignorer); err != nil {
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
			c.Status = kstatus.Wrap(&k8sv1.GatewayClassStatus{})
		case gvk.KubernetesGateway:
			c.Status = kstatus.Wrap(&k8sv1.GatewayStatus{})
		case gvk.HTTPRoute:
			c.Status = kstatus.Wrap(&k8sv1.HTTPRouteStatus{})
		case gvk.GRPCRoute:
			c.Status = kstatus.Wrap(&k8s.GRPCRouteStatus{})
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

func BenchmarkBuildHTTPVirtualServices(b *testing.B) {
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
			ClusterExternalAddresses: &model.AddressMap{
				Addresses: map[cluster.ID][]string{
					constants.DefaultClusterName: {"1.2.3.4"},
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
	cg := core.NewConfigGenTest(b, core.TestOptions{
		Services: []*model.Service{ingressSvc, altIngressSvc},
		Instances: []*model.ServiceInstance{
			{Service: ingressSvc, ServicePort: ingressSvc.Ports[0], Endpoint: &model.IstioEndpoint{EndpointPort: 8080}},
			{Service: ingressSvc, ServicePort: ingressSvc.Ports[1], Endpoint: &model.IstioEndpoint{}},
			{Service: altIngressSvc, ServicePort: altIngressSvc.Ports[0], Endpoint: &model.IstioEndpoint{}},
			{Service: altIngressSvc, ServicePort: altIngressSvc.Ports[1], Endpoint: &model.IstioEndpoint{}},
		},
	})

	validator := crdvalidation.NewIstioValidator(b)
	input := readConfig(b, "testdata/benchmark-httproute.yaml", validator, nil)
	kr := splitInput(b, input)
	kr.Context = NewGatewayContext(cg.PushContext(), "Kubernetes")
	ctx := configContext{
		GatewayResources:  kr,
		AllowedReferences: convertReferencePolicies(kr),
	}
	_, gwMap, _ := convertGateways(ctx)
	ctx.GatewayReferences = gwMap

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		// for gateway routes, build one VS per gateway+host
		gatewayRoutes := make(map[string]map[string]*config.Config)
		// for mesh routes, build one VS per namespace+host
		meshRoutes := make(map[string]map[string]*config.Config)
		for _, obj := range kr.HTTPRoute {
			buildHTTPVirtualServices(ctx, obj, gatewayRoutes, meshRoutes)
		}
	}
}

func TestExtractGatewayServices(t *testing.T) {
	tests := []struct {
		name            string
		r               GatewayResources
		kgw             *k8sv1.GatewaySpec
		obj             config.Config
		gatewayServices []string
		err             *ConfigError
	}{
		{
			name: "managed gateway",
			r:    GatewayResources{Domain: "cluster.local"},
			kgw: &k8sv1.GatewaySpec{
				GatewayClassName: "istio",
			},
			obj: config.Config{
				Meta: config.Meta{
					Name:      "foo",
					Namespace: "default",
				},
			},
			gatewayServices: []string{"foo-istio.default.svc.cluster.local"},
		},
		{
			name: "managed gateway with name overridden",
			r:    GatewayResources{Domain: "cluster.local"},
			kgw: &k8sv1.GatewaySpec{
				GatewayClassName: "istio",
			},
			obj: config.Config{
				Meta: config.Meta{
					Name:      "foo",
					Namespace: "default",
					Annotations: map[string]string{
						gatewayNameOverride: "bar",
					},
				},
			},
			gatewayServices: []string{"bar.default.svc.cluster.local"},
		},
		{
			name: "unmanaged gateway",
			r:    GatewayResources{Domain: "domain"},
			kgw: &k8sv1.GatewaySpec{
				GatewayClassName: "istio",
				Addresses: []k8sv1.GatewayAddress{
					{
						Value: "abc",
					},
					{
						Type: func() *k8sv1.AddressType {
							t := k8sv1.HostnameAddressType
							return &t
						}(),
						Value: "example.com",
					},
					{
						Type: func() *k8sv1.AddressType {
							t := k8sv1.IPAddressType
							return &t
						}(),
						Value: "1.2.3.4",
					},
				},
			},
			obj: config.Config{
				Meta: config.Meta{
					Name:      "foo",
					Namespace: "default",
				},
			},
			gatewayServices: []string{"abc.default.svc.domain", "example.com"},
			err: &ConfigError{
				Reason:  InvalidAddress,
				Message: "only Hostname is supported, ignoring [1.2.3.4]",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayServices, err := extractGatewayServices(tt.r, tt.kgw, tt.obj)
			assert.Equal(t, gatewayServices, tt.gatewayServices)
			assert.Equal(t, err, tt.err)
		})
	}
}
