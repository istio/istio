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

package model

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	securityBeta "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"

	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
)

const (
	rootNamespace = "istio-config"
)

var (
	baseTimestamp = time.Date(2020, 2, 2, 2, 2, 2, 0, time.UTC)
)

func TestSdsCertificateConfigFromResourceName(t *testing.T) {
	cases := []struct {
		name             string
		resource         string
		root             bool
		key              bool
		output           SdsCertificateConfig
		rootResourceName string
		resourceName     string
	}{
		{
			"cert",
			"file-cert:cert~key",
			false,
			true,
			SdsCertificateConfig{"cert", "key", ""},
			"",
			"file-cert:cert~key",
		},
		{
			"root cert",
			"file-root:root",
			true,
			false,
			SdsCertificateConfig{"", "", "root"},
			"file-root:root",
			"",
		},
		{
			"invalid prefix",
			"file:root",
			false,
			false,
			SdsCertificateConfig{"", "", ""},
			"",
			"",
		},
		{
			"invalid contents",
			"file-root:root~extra",
			false,
			false,
			SdsCertificateConfig{"", "", ""},
			"",
			"",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := SdsCertificateConfigFromResourceName(tt.resource)
			if got != tt.output {
				t.Fatalf("got %v, expected %v", got, tt.output)
			}
			if root := got.IsRootCertificate(); root != tt.root {
				t.Fatalf("unexpected isRootCertificate got %v, expected %v", root, tt.root)
			}
			if key := got.IsKeyCertificate(); key != tt.key {
				t.Fatalf("unexpected IsKeyCertificate got %v, expected %v", key, tt.key)
			}
			if root := got.GetRootResourceName(); root != tt.rootResourceName {
				t.Fatalf("unexpected GetRootResourceName got %v, expected %v", root, tt.rootResourceName)
			}
			if cert := got.GetResourceName(); cert != tt.resourceName {
				t.Fatalf("unexpected GetRootResourceName got %v, expected %v", cert, tt.resourceName)
			}
		})
	}
}

func TestGetPoliciesForWorkload(t *testing.T) {
	policies := getTestAuthenticationPolicies(createTestConfigs(true /* with mesh peer authn */), t)

	cases := []struct {
		name                   string
		workloadNamespace      string
		workloadLabels         labels.Collection
		wantRequestAuthn       []*Config
		wantPeerAuthn          []*Config
		wantNamespaceMutualTLS MutualTLSMode
	}{
		{
			name:              "Empty workload labels in foo",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{},
			wantRequestAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "foo",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "istio-config",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_UNSET,
						},
					},
				},
			},
			wantNamespaceMutualTLS: MTLSStrict,
		},
		{
			name:              "Empty workload labels in bar",
			workloadNamespace: "bar",
			workloadLabels:    labels.Collection{},
			wantRequestAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "bar",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "istio-config",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_UNSET,
						},
					},
				},
			},
			wantNamespaceMutualTLS: MTLSPermissive,
		},
		{
			name:              "Empty workload labels in baz",
			workloadNamespace: "baz",
			workloadLabels:    labels.Collection{},
			wantRequestAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "istio-config",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_UNSET,
						},
					},
				},
			},
			wantNamespaceMutualTLS: MTLSPermissive,
		},
		{
			name:              "Match workload labels in foo",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{{"app": "httpbin", "version": "v1", "other": "labels"}},
			wantRequestAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "foo",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "with-selector",
						Namespace:        "foo",
					},
					Spec: &securityBeta.RequestAuthentication{
						Selector: &selectorpb.WorkloadSelector{
							MatchLabels: map[string]string{
								"app":     "httpbin",
								"version": "v1",
							},
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "global-with-selector",
						Namespace:        "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{
						Selector: &selectorpb.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "httpbin",
							},
						},
					},
				},
			},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "peer-with-selector",
						Namespace:         "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Selector: &selectorpb.WorkloadSelector{
							MatchLabels: map[string]string{
								"version": "v1",
							},
						},
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_DISABLE,
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "istio-config",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_UNSET,
						},
					},
				},
			},
			wantNamespaceMutualTLS: MTLSStrict,
		},
		{
			name:              "Match workload labels in bar",
			workloadNamespace: "bar",
			workloadLabels:    labels.Collection{{"app": "httpbin", "version": "v1"}},
			wantRequestAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "bar",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "global-with-selector",
						Namespace:        "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{
						Selector: &selectorpb.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "httpbin",
							},
						},
					},
				},
			},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "istio-config",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_UNSET,
						},
					},
				},
			},
			wantNamespaceMutualTLS: MTLSPermissive,
		},
		{
			name:              "Paritial match workload labels in foo",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{{"app": "httpbin"}},
			wantRequestAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "foo",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "global-with-selector",
						Namespace:        "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{
						Selector: &selectorpb.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "httpbin",
							},
						},
					},
				},
			},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "istio-config",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_UNSET,
						},
					},
				},
			},
			wantNamespaceMutualTLS: MTLSStrict,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policies.GetJwtPoliciesForWorkload(tc.workloadNamespace, tc.workloadLabels); !reflect.DeepEqual(tc.wantRequestAuthn, got) {
				t.Fatalf("want %+v\n, but got %+v\n", printConfigs(tc.wantRequestAuthn), printConfigs(got))
			}
			if got := policies.GetPeerAuthenticationsForWorkload(tc.workloadNamespace, tc.workloadLabels); !reflect.DeepEqual(tc.wantPeerAuthn, got) {
				t.Fatalf("want %+v\n, but got %+v\n", printConfigs(tc.wantPeerAuthn), printConfigs(got))
			}
			if got := policies.GetNamespaceMutualTLSMode(tc.workloadNamespace); got != tc.wantNamespaceMutualTLS {
				t.Fatalf("want %s\n, but got %s\n", tc.wantNamespaceMutualTLS, got)
			}
		})
	}
}

func TestGetPoliciesForWorkloadWithoutMeshPeerAuthn(t *testing.T) {
	policies := getTestAuthenticationPolicies(createTestConfigs(false /* with mesh peer authn */), t)

	cases := []struct {
		name                   string
		workloadNamespace      string
		workloadLabels         labels.Collection
		wantPeerAuthn          []*Config
		wantNamespaceMutualTLS MutualTLSMode
	}{
		{
			name:              "Empty workload labels in foo",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
			},
			wantNamespaceMutualTLS: MTLSStrict,
		},
		{
			name:                   "Empty workload labels in bar",
			workloadNamespace:      "bar",
			workloadLabels:         labels.Collection{},
			wantPeerAuthn:          []*Config{},
			wantNamespaceMutualTLS: MTLSUnknown,
		},
		{
			name:                   "Empty workload labels in baz",
			workloadNamespace:      "baz",
			workloadLabels:         labels.Collection{},
			wantPeerAuthn:          []*Config{},
			wantNamespaceMutualTLS: MTLSUnknown,
		},
		{
			name:              "Match workload labels in foo",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{{"app": "httpbin", "version": "v1", "other": "labels"}},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "peer-with-selector",
						Namespace:         "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Selector: &selectorpb.WorkloadSelector{
							MatchLabels: map[string]string{
								"version": "v1",
							},
						},
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_DISABLE,
						},
					},
				},
			},
			wantNamespaceMutualTLS: MTLSStrict,
		},
		{
			name:                   "Match workload labels in bar",
			workloadNamespace:      "bar",
			workloadLabels:         labels.Collection{{"app": "httpbin", "version": "v1"}},
			wantPeerAuthn:          []*Config{},
			wantNamespaceMutualTLS: MTLSUnknown,
		},
		{
			name:              "Paritial match workload labels in foo",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{{"app": "httpbin"}},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind:  gvk.PeerAuthentication,
						CreationTimestamp: baseTimestamp,
						Name:              "default",
						Namespace:         "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
			},
			wantNamespaceMutualTLS: MTLSStrict,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policies.GetPeerAuthenticationsForWorkload(tc.workloadNamespace, tc.workloadLabels); !reflect.DeepEqual(tc.wantPeerAuthn, got) {
				t.Fatalf("want %+v\n, but got %+v\n", printConfigs(tc.wantPeerAuthn), printConfigs(got))
			}
			if got := policies.GetNamespaceMutualTLSMode(tc.workloadNamespace); got != tc.wantNamespaceMutualTLS {
				t.Fatalf("want %s, but got %s", tc.wantNamespaceMutualTLS, got)
			}
		})
	}
}

func TestGetPoliciesForWorkloadWithJwksResolver(t *testing.T) {
	ms, err := test.StartNewServer()
	defer ms.Stop()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	mockCertURL := ms.URL + "/oauth2/v3/certs"

	policies := getTestAuthenticationPolicies(createNonTrivialRequestAuthnTestConfigs(ms.URL), t)

	cases := []struct {
		name              string
		workloadNamespace string
		workloadLabels    labels.Collection
		wantRequestAuthn  []*Config
	}{
		{
			name:              "single hit",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{},
			wantRequestAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        rootNamespace,
					},
					Spec: &securityBeta.RequestAuthentication{
						JwtRules: []*securityBeta.JWTRule{
							{
								Issuer:  ms.URL,
								JwksUri: mockCertURL,
							},
						},
					},
				},
			},
		},
		{
			name:              "double hit",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{{"app": "httpbin"}},
			wantRequestAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        rootNamespace,
					},
					Spec: &securityBeta.RequestAuthentication{
						JwtRules: []*securityBeta.JWTRule{
							{
								Issuer:  ms.URL,
								JwksUri: mockCertURL,
							},
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "global-with-selector",
						Namespace:        rootNamespace,
					},
					Spec: &securityBeta.RequestAuthentication{
						Selector: &selectorpb.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "httpbin",
							},
						},
						JwtRules: []*securityBeta.JWTRule{
							{
								Issuer:  ms.URL,
								JwksUri: mockCertURL,
							},
							{
								Issuer: "bad-issuer",
							},
						},
					},
				},
			},
		},
		{
			name:              "tripple hit",
			workloadNamespace: "foo",
			workloadLabels:    labels.Collection{{"app": "httpbin", "version": "v1"}},
			wantRequestAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "with-selector",
						Namespace:        "foo",
					},
					Spec: &securityBeta.RequestAuthentication{
						Selector: &selectorpb.WorkloadSelector{
							MatchLabels: map[string]string{
								"app":     "httpbin",
								"version": "v1",
							},
						},
						JwtRules: []*securityBeta.JWTRule{
							{
								Issuer:  "issuer-with-jwks-uri",
								JwksUri: "example.com",
							},
							{
								Issuer: "issuer-with-jwks",
								Jwks:   "deadbeef",
							},
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "default",
						Namespace:        rootNamespace,
					},
					Spec: &securityBeta.RequestAuthentication{
						JwtRules: []*securityBeta.JWTRule{
							{
								Issuer:  ms.URL,
								JwksUri: mockCertURL,
							},
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						GroupVersionKind: gvk.RequestAuthentication,
						Name:             "global-with-selector",
						Namespace:        rootNamespace,
					},
					Spec: &securityBeta.RequestAuthentication{
						Selector: &selectorpb.WorkloadSelector{
							MatchLabels: map[string]string{
								"app": "httpbin",
							},
						},
						JwtRules: []*securityBeta.JWTRule{
							{
								Issuer:  ms.URL,
								JwksUri: mockCertURL,
							},
							{
								Issuer: "bad-issuer",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policies.GetJwtPoliciesForWorkload(tc.workloadNamespace, tc.workloadLabels); !reflect.DeepEqual(tc.wantRequestAuthn, got) {
				t.Fatalf("want %+v\n, but got %+v\n", printConfigs(tc.wantRequestAuthn), printConfigs(got))
			}
		})
	}
}

func getTestAuthenticationPolicies(configs []*Config, t *testing.T) *AuthenticationPolicies {
	configStore := NewFakeStore()
	for _, cfg := range configs {
		log.Infof("add config %s\n", cfg.Name)
		if _, err := configStore.Create(*cfg); err != nil {
			t.Fatalf("getTestAuthenticationPolicies %v", err)
		}
	}
	environment := &Environment{
		IstioConfigStore: MakeIstioStore(configStore),
		Watcher:          mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: rootNamespace}),
	}
	authnPolicy, err := initAuthenticationPolicies(environment)
	if err != nil {
		t.Fatalf("getTestAuthenticationPolicies %v", err)
	}

	return authnPolicy
}

func createTestRequestAuthenticationResource(name string, namespace string, selector *selectorpb.WorkloadSelector) *Config {
	return &Config{
		ConfigMeta: ConfigMeta{
			GroupVersionKind: collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind(),
			Name:             name,
			Namespace:        namespace,
		},
		Spec: &securityBeta.RequestAuthentication{
			Selector: selector,
		},
	}
}

func createTestPeerAuthenticationResource(name string, namespace string, timestamp time.Time,
	selector *selectorpb.WorkloadSelector, mode securityBeta.PeerAuthentication_MutualTLS_Mode) *Config {
	return &Config{
		ConfigMeta: ConfigMeta{
			GroupVersionKind:  collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind(),
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: timestamp,
		},
		Spec: &securityBeta.PeerAuthentication{
			Selector: selector,
			Mtls: &securityBeta.PeerAuthentication_MutualTLS{
				Mode: mode,
			},
		},
	}
}

func createTestConfigs(withMeshPeerAuthn bool) []*Config {
	configs := make([]*Config, 0)

	selector := &selectorpb.WorkloadSelector{
		MatchLabels: map[string]string{
			"app":     "httpbin",
			"version": "v1",
		},
	}
	configs = append(configs, createTestRequestAuthenticationResource("default", rootNamespace, nil),
		createTestRequestAuthenticationResource("global-with-selector", rootNamespace, &selectorpb.WorkloadSelector{
			MatchLabels: map[string]string{
				"app": "httpbin",
			},
		}),
		createTestRequestAuthenticationResource("default", "foo", nil),
		createTestRequestAuthenticationResource("default", "bar", nil),
		createTestRequestAuthenticationResource("with-selector", "foo", selector),
		createTestPeerAuthenticationResource("global-peer-with-selector", rootNamespace, baseTimestamp, &selectorpb.WorkloadSelector{
			MatchLabels: map[string]string{
				"app":     "httpbin",
				"version": "v2",
			},
		}, securityBeta.PeerAuthentication_MutualTLS_UNSET),
		createTestPeerAuthenticationResource("default", "foo", baseTimestamp, nil, securityBeta.PeerAuthentication_MutualTLS_STRICT),
		createTestPeerAuthenticationResource("ignored-newer-default", "foo", baseTimestamp.Add(time.Second), nil, securityBeta.PeerAuthentication_MutualTLS_STRICT),
		createTestPeerAuthenticationResource("peer-with-selector", "foo", baseTimestamp, &selectorpb.WorkloadSelector{
			MatchLabels: map[string]string{
				"version": "v1",
			},
		}, securityBeta.PeerAuthentication_MutualTLS_DISABLE))

	if withMeshPeerAuthn {
		configs = append(configs,
			createTestPeerAuthenticationResource("ignored-newer", rootNamespace, baseTimestamp.Add(time.Second*2),
				nil, securityBeta.PeerAuthentication_MutualTLS_UNSET),
			createTestPeerAuthenticationResource("default", rootNamespace, baseTimestamp,
				nil, securityBeta.PeerAuthentication_MutualTLS_UNSET),
			createTestPeerAuthenticationResource("ignored-another-newer", rootNamespace, baseTimestamp.Add(time.Second),
				nil, securityBeta.PeerAuthentication_MutualTLS_UNSET))
	}

	return configs
}

func addJwtRule(issuer, jwksURI, jwks string, config *Config) {
	spec := config.Spec.(*securityBeta.RequestAuthentication)
	if spec.JwtRules == nil {
		spec.JwtRules = make([]*securityBeta.JWTRule, 0)
	}
	spec.JwtRules = append(spec.JwtRules, &securityBeta.JWTRule{
		Issuer:  issuer,
		JwksUri: jwksURI,
		Jwks:    jwks,
	})
}

func createNonTrivialRequestAuthnTestConfigs(issuer string) []*Config {
	configs := make([]*Config, 0)

	globalCfg := createTestRequestAuthenticationResource("default", rootNamespace, nil)
	addJwtRule(issuer, "", "", globalCfg)
	configs = append(configs, globalCfg)

	httpbinCfg :=
		createTestRequestAuthenticationResource("global-with-selector", rootNamespace, &selectorpb.WorkloadSelector{
			MatchLabels: map[string]string{
				"app": "httpbin",
			},
		})

	addJwtRule(issuer, "", "", httpbinCfg)
	addJwtRule("bad-issuer", "", "", httpbinCfg)
	configs = append(configs, httpbinCfg)

	httpbinCfgV1 := createTestRequestAuthenticationResource("with-selector", "foo", &selectorpb.WorkloadSelector{
		MatchLabels: map[string]string{
			"app":     "httpbin",
			"version": "v1",
		},
	})
	addJwtRule("issuer-with-jwks-uri", "example.com", "", httpbinCfgV1)
	addJwtRule("issuer-with-jwks", "", "deadbeef", httpbinCfgV1)
	configs = append(configs, httpbinCfgV1)

	return configs
}

func printConfigs(configs []*Config) string {
	s := "[\n"
	for _, c := range configs {
		s += fmt.Sprintf("%+v\n", c)
	}
	return s + "]"
}
