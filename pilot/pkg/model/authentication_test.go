// Copyright 2019 Istio Authors
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

	meshconfig "istio.io/api/mesh/v1alpha1"
	securityBeta "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
)

const (
	rootNamespace = "istio-config"
)

var (
	peerAuthenticationGvk    = collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind()
	requestAuthenticationGvk = collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind()
)

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
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "foo",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
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
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "bar",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
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
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
			},
			wantPeerAuthn: []*Config{
				{
					ConfigMeta: ConfigMeta{
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
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
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "foo",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "with-selector",
						Namespace: "foo",
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
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "global-with-selector",
						Namespace: "istio-config",
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
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "peer-with-selector",
						Namespace: "foo",
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
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
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
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "bar",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "global-with-selector",
						Namespace: "istio-config",
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
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
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
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "foo",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
					},
					Spec: &securityBeta.RequestAuthentication{},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      requestAuthenticationGvk.Kind,
						Version:   requestAuthenticationGvk.Version,
						Group:     requestAuthenticationGvk.Group,
						Name:      "global-with-selector",
						Namespace: "istio-config",
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
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "istio-config",
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
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "foo",
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
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "foo",
					},
					Spec: &securityBeta.PeerAuthentication{
						Mtls: &securityBeta.PeerAuthentication_MutualTLS{
							Mode: securityBeta.PeerAuthentication_MutualTLS_STRICT,
						},
					},
				},
				{
					ConfigMeta: ConfigMeta{
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "peer-with-selector",
						Namespace: "foo",
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
						Type:      peerAuthenticationGvk.Kind,
						Version:   peerAuthenticationGvk.Version,
						Group:     peerAuthenticationGvk.Group,
						Name:      "default",
						Namespace: "foo",
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

func getTestAuthenticationPolicies(configs []*Config, t *testing.T) *AuthenticationPolicies {
	configStore := newFakeStore()
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
			Type:      collections.IstioSecurityV1Beta1Requestauthentications.Resource().Kind(),
			Version:   collections.IstioSecurityV1Beta1Requestauthentications.Resource().Version(),
			Group:     collections.IstioSecurityV1Beta1Requestauthentications.Resource().Group(),
			Name:      name,
			Namespace: namespace,
		},
		Spec: &securityBeta.RequestAuthentication{
			Selector: selector,
		},
	}
}

func createTestPeerAuthenticationResource(name string, namespace string,
	selector *selectorpb.WorkloadSelector, mode securityBeta.PeerAuthentication_MutualTLS_Mode) *Config {
	return &Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioSecurityV1Beta1Peerauthentications.Resource().Kind(),
			Version:   collections.IstioSecurityV1Beta1Peerauthentications.Resource().Version(),
			Group:     collections.IstioSecurityV1Beta1Peerauthentications.Resource().Group(),
			Name:      name,
			Namespace: namespace,
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
		createTestPeerAuthenticationResource("global-peer-with-selector", rootNamespace, &selectorpb.WorkloadSelector{
			MatchLabels: map[string]string{
				"app":     "httpbin",
				"version": "v2",
			},
		}, securityBeta.PeerAuthentication_MutualTLS_UNSET),
		createTestPeerAuthenticationResource("default", "foo", nil, securityBeta.PeerAuthentication_MutualTLS_STRICT),
		createTestPeerAuthenticationResource("peer-with-selector", "foo", &selectorpb.WorkloadSelector{
			MatchLabels: map[string]string{
				"version": "v1",
			},
		}, securityBeta.PeerAuthentication_MutualTLS_DISABLE))

	if withMeshPeerAuthn {
		configs = append(configs, createTestPeerAuthenticationResource("default", rootNamespace, nil, securityBeta.PeerAuthentication_MutualTLS_UNSET))
	}

	return configs
}

func printConfigs(configs []*Config) string {
	s := "[\n"
	for _, c := range configs {
		s += fmt.Sprintf("%+v\n", c)
	}
	return s + "]"
}
