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
	"regexp"
	"testing"
	"time"

	authn "istio.io/api/authentication/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	securityBeta "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"

	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
)

func TestMergeUpdateRequest(t *testing.T) {
	push0 := &PushContext{}
	// trivially different push contexts just for testing
	push1 := &PushContext{ProxyStatus: make(map[string]map[string]ProxyPushStatus)}

	var t0 time.Time
	t1 := t0.Add(time.Minute)

	cases := []struct {
		name   string
		left   *PushRequest
		right  *PushRequest
		merged PushRequest
	}{
		{
			"left nil",
			nil,
			&PushRequest{Full: true},
			PushRequest{Full: true},
		},
		{
			"right nil",
			&PushRequest{Full: true},
			nil,
			PushRequest{Full: true},
		},
		{
			"simple merge",
			&PushRequest{
				Full:               true,
				Push:               push0,
				Start:              t0,
				NamespacesUpdated:  map[string]struct{}{"ns1": {}},
				ConfigTypesUpdated: map[resource.GroupVersionKind]struct{}{{Kind: "cfg1"}: {}},
				Reason:             []TriggerReason{ServiceUpdate, ServiceUpdate},
			},
			&PushRequest{
				Full:               false,
				Push:               push1,
				Start:              t1,
				NamespacesUpdated:  map[string]struct{}{"ns2": {}},
				ConfigTypesUpdated: map[resource.GroupVersionKind]struct{}{{Kind: "cfg2"}: {}},
				Reason:             []TriggerReason{EndpointUpdate},
			},
			PushRequest{
				Full:               true,
				Push:               push1,
				Start:              t0,
				NamespacesUpdated:  map[string]struct{}{"ns1": {}, "ns2": {}},
				ConfigTypesUpdated: map[resource.GroupVersionKind]struct{}{{Kind: "cfg1"}: {}, {Kind: "cfg2"}: {}},
				Reason:             []TriggerReason{ServiceUpdate, ServiceUpdate, EndpointUpdate},
			},
		},
		{
			"incremental eds merge",
			&PushRequest{Full: false, EdsUpdates: map[string]struct{}{"svc-1": {}}},
			&PushRequest{Full: false, EdsUpdates: map[string]struct{}{"svc-2": {}}},
			PushRequest{Full: false, EdsUpdates: map[string]struct{}{"svc-1": {}, "svc-2": {}}},
		},
		{
			"skip eds merge: left full",
			&PushRequest{Full: true},
			&PushRequest{Full: false, EdsUpdates: map[string]struct{}{"svc-2": {}}},
			PushRequest{Full: true},
		},
		{
			"skip eds merge: right full",
			&PushRequest{Full: false, EdsUpdates: map[string]struct{}{"svc-1": {}}},
			&PushRequest{Full: true},
			PushRequest{Full: true},
		},
		{
			"incremental merge",
			&PushRequest{Full: false, NamespacesUpdated: map[string]struct{}{"ns1": {}}, EdsUpdates: map[string]struct{}{"svc-1": {}}},
			&PushRequest{Full: false, NamespacesUpdated: map[string]struct{}{"ns2": {}}, EdsUpdates: map[string]struct{}{"svc-2": {}}},
			PushRequest{Full: false, NamespacesUpdated: map[string]struct{}{"ns1": {}, "ns2": {}}, EdsUpdates: map[string]struct{}{"svc-1": {}, "svc-2": {}}},
		},
		{
			"skip namespace merge: one empty",
			&PushRequest{Full: true, NamespacesUpdated: nil},
			&PushRequest{Full: true, NamespacesUpdated: map[string]struct{}{"ns2": {}}},
			PushRequest{Full: true, NamespacesUpdated: nil},
		},
		{
			"skip config type merge: one empty",
			&PushRequest{Full: true, ConfigTypesUpdated: nil},
			&PushRequest{Full: true, ConfigTypesUpdated: map[resource.GroupVersionKind]struct{}{{Kind: "cfg2"}: {}}},
			PushRequest{Full: true, ConfigTypesUpdated: nil},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.left.Merge(tt.right)
			if !reflect.DeepEqual(&tt.merged, got) {
				t.Fatalf("expected %v, got %v", tt.merged, got)
			}
		})
	}
}

func TestAuthNPolicies(t *testing.T) {
	const testNamespace string = "test-namespace"
	ps := NewPushContext()
	env := &Environment{Watcher: mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})}
	ps.Mesh = env.Mesh()
	ps.ServiceDiscovery = env
	authNPolicies := map[string]*authn.Policy{
		constants.DefaultAuthenticationPolicyName: {},

		"mtls-strict-svc": {
			Targets: []*authn.TargetSelector{{
				Name: "mtls-strict-svc-port",
			}},
			Peers: []*authn.PeerAuthenticationMethod{{
				Params: &authn.PeerAuthenticationMethod_Mtls{},
			},
			}},

		"mtls-strict-svc-port": {
			Targets: []*authn.TargetSelector{{
				Name: "mtls-strict-svc-port",
				Ports: []*authn.PortSelector{
					{
						Port: &authn.PortSelector_Number{
							Number: 80,
						},
					},
				},
			}},
			Peers: []*authn.PeerAuthenticationMethod{{
				Params: &authn.PeerAuthenticationMethod_Mtls{},
			},
			}},

		"mtls-permissive-svc-port": {
			Targets: []*authn.TargetSelector{{
				Name: "mtls-permissive-svc-port",
				Ports: []*authn.PortSelector{
					{
						Port: &authn.PortSelector_Number{
							Number: 80,
						},
					},
				},
			}},
			Peers: []*authn.PeerAuthenticationMethod{{
				Params: &authn.PeerAuthenticationMethod_Mtls{
					Mtls: &authn.MutualTls{
						Mode: authn.MutualTls_PERMISSIVE,
					},
				},
			}},
		},

		"mtls-strict-svc-named-port": {
			Targets: []*authn.TargetSelector{{
				Name: "mtls-strict-svc-named-port",
				Ports: []*authn.PortSelector{
					{
						Port: &authn.PortSelector_Name{
							Name: "http",
						},
					},
				},
			}},
			Peers: []*authn.PeerAuthenticationMethod{{
				Params: &authn.PeerAuthenticationMethod_Mtls{},
			}},
		},

		"mtls-disable-svc": {
			Targets: []*authn.TargetSelector{{
				Name: "mtls-disable-svc",
			}},
		},
	}
	configStore := newFakeStore()
	for key, value := range authNPolicies {
		cfg := Config{
			ConfigMeta: ConfigMeta{
				Type:      collections.IstioAuthenticationV1Alpha1Policies.Resource().Kind(),
				Group:     collections.IstioAuthenticationV1Alpha1Policies.Resource().Group(),
				Version:   collections.IstioAuthenticationV1Alpha1Policies.Resource().Version(),
				Name:      key,
				Domain:    "cluster.local",
				Namespace: testNamespace,
			},
			Spec: value,
		}
		if _, err := configStore.Create(cfg); err != nil {
			t.Error(err)
		}
	}

	// Add cluster-scoped policy
	globalPolicy := &authn.Policy{
		Peers: []*authn.PeerAuthenticationMethod{{
			Params: &authn.PeerAuthenticationMethod_Mtls{
				Mtls: &authn.MutualTls{
					Mode: authn.MutualTls_PERMISSIVE,
				},
			},
		}},
	}
	globalCfg := Config{
		ConfigMeta: ConfigMeta{
			Type:    collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().Kind(),
			Group:   collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().Group(),
			Version: collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().Version(),
			Name:    constants.DefaultAuthenticationPolicyName,
			Domain:  "cluster.local",
		},
		Spec: globalPolicy,
	}
	if _, err := configStore.Create(globalCfg); err != nil {
		t.Error(err)
	}

	store := istioConfigStore{ConfigStore: configStore}
	env.IstioConfigStore = &store
	if err := ps.initAuthnPolicies(env); err != nil {
		t.Fatalf("init authn policies failed: %v", err)
	}

	cases := []struct {
		hostname                host.Name
		namespace               string
		port                    Port
		expectedPolicy          *authn.Policy
		expectedPolicyName      string
		expectedPolicyNamespace string
	}{
		{
			hostname:                "mtls-strict-svc-port.test-namespace.svc.cluster.local",
			namespace:               testNamespace,
			port:                    Port{Port: 80},
			expectedPolicy:          authNPolicies["mtls-strict-svc-port"],
			expectedPolicyName:      "mtls-strict-svc-port",
			expectedPolicyNamespace: testNamespace,
		},
		{
			hostname:                "mtls-strict-svc-port.test-namespace.svc.cluster.local",
			namespace:               testNamespace,
			port:                    Port{Port: 90},
			expectedPolicy:          authNPolicies["mtls-strict-svc"],
			expectedPolicyName:      "mtls-strict-svc",
			expectedPolicyNamespace: testNamespace,
		},
		{
			hostname:                "mtls-permissive-svc-port.test-namespace.svc.cluster.local",
			namespace:               testNamespace,
			port:                    Port{Port: 80},
			expectedPolicy:          authNPolicies["mtls-permissive-svc-port"],
			expectedPolicyName:      "mtls-permissive-svc-port",
			expectedPolicyNamespace: testNamespace,
		},
		{
			hostname:                "mtls-permissive-svc-port.test-namespace.svc.cluster.local",
			namespace:               testNamespace,
			port:                    Port{Port: 90},
			expectedPolicy:          authNPolicies[constants.DefaultAuthenticationPolicyName],
			expectedPolicyName:      constants.DefaultAuthenticationPolicyName,
			expectedPolicyNamespace: testNamespace,
		},
		{
			hostname:                "mtls-disable-svc.test-namespace.svc.cluster.local",
			namespace:               testNamespace,
			port:                    Port{Port: 80},
			expectedPolicy:          authNPolicies["mtls-disable-svc"],
			expectedPolicyName:      "mtls-disable-svc",
			expectedPolicyNamespace: testNamespace,
		},
		{
			hostname:                "mtls-strict-svc-port.another-namespace.svc.cluster.local",
			namespace:               "another-namespace",
			port:                    Port{Port: 80},
			expectedPolicy:          globalPolicy,
			expectedPolicyName:      constants.DefaultAuthenticationPolicyName,
			expectedPolicyNamespace: NamespaceAll,
		},
		{
			hostname:                "mtls-default-svc-port.test-namespace.svc.cluster.local",
			namespace:               testNamespace,
			port:                    Port{Port: 80},
			expectedPolicy:          authNPolicies[constants.DefaultAuthenticationPolicyName],
			expectedPolicyName:      constants.DefaultAuthenticationPolicyName,
			expectedPolicyNamespace: testNamespace,
		},
		{
			hostname:                "mtls-strict-svc-named-port.test-namespace.svc.cluster.local",
			namespace:               testNamespace,
			port:                    Port{Name: "http"},
			expectedPolicy:          authNPolicies["mtls-strict-svc-named-port"],
			expectedPolicyName:      "mtls-strict-svc-named-port",
			expectedPolicyNamespace: testNamespace,
		},
	}

	for _, c := range cases {
		ps.ServiceByHostnameAndNamespace[c.hostname] = map[string]*Service{"default": nil}
	}

	for i, c := range cases {
		service := &Service{
			Hostname:   c.hostname,
			Attributes: ServiceAttributes{Namespace: c.namespace},
		}
		testName := fmt.Sprintf("%d. %s.%s:%v", i, c.hostname, c.namespace, c.port)
		t.Run(testName, func(t *testing.T) {
			gotPolicy, gotMeta := ps.AuthenticationPolicyForWorkload(service, &c.port)
			if gotMeta.Name != c.expectedPolicyName || gotMeta.Namespace != c.expectedPolicyNamespace {
				t.Errorf("Config meta: got \"%s@%s\" != want(\"%s@%s\")\n",
					gotMeta.Name, gotMeta.Namespace, c.expectedPolicyName, c.expectedPolicyNamespace)
			}

			if !reflect.DeepEqual(gotPolicy, c.expectedPolicy) {
				t.Errorf("Policy: got(%v) != want(%v)\n", gotPolicy, c.expectedPolicy)
			}
		})
	}
}

func TestJwtAuthNPolicy(t *testing.T) {
	ms, err := test.StartNewServer()
	defer func() { _ = ms.Stop() }()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}

	ps := NewPushContext()
	env := &Environment{Watcher: mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})}
	ps.Mesh = env.Mesh()
	ps.ServiceDiscovery = env
	authNPolicies := map[string]*authn.Policy{
		constants.DefaultAuthenticationPolicyName: {},

		"jwt-with-jwks-uri": {
			Targets: []*authn.TargetSelector{{
				Name: "jwt-svc-1",
			}},
			Origins: []*authn.OriginAuthenticationMethod{{
				Jwt: &authn.Jwt{
					Issuer:  "http://abc",
					JwksUri: "http://xyz",
				},
			}},
		},
		"jwt-without-jwks-uri": {
			Targets: []*authn.TargetSelector{{
				Name: "jwt-svc-2",
			}},
			Origins: []*authn.OriginAuthenticationMethod{{
				Jwt: &authn.Jwt{
					Issuer: ms.URL,
				},
			}},
		},
	}

	configStore := newFakeStore()
	for key, value := range authNPolicies {
		cfg := Config{
			ConfigMeta: ConfigMeta{
				Name:      key,
				Domain:    "cluster.local",
				Namespace: "default",
			},
			Spec: value,
		}
		if key == constants.DefaultAuthenticationPolicyName {
			// Cluster-scoped policy
			cfg.ConfigMeta.Type = collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().Kind()
			cfg.ConfigMeta.Version = collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().Version()
			cfg.ConfigMeta.Group = collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().Group()
			cfg.ConfigMeta.Namespace = NamespaceAll
		} else {
			cfg.ConfigMeta.Type = collections.IstioAuthenticationV1Alpha1Policies.Resource().Kind()
			cfg.ConfigMeta.Version = collections.IstioAuthenticationV1Alpha1Policies.Resource().Version()
			cfg.ConfigMeta.Group = collections.IstioAuthenticationV1Alpha1Policies.Resource().Group()
		}
		if _, err := configStore.Create(cfg); err != nil {
			t.Error(err)
		}
	}

	store := istioConfigStore{ConfigStore: configStore}
	env.IstioConfigStore = &store
	if err := ps.initAuthnPolicies(env); err != nil {
		t.Fatalf("init authn policies failed: %v", err)
	}

	cases := []struct {
		hostname        host.Name
		namespace       string
		port            Port
		expectedJwksURI string
	}{
		{
			hostname:        "jwt-svc-1.default.svc.cluster.local",
			namespace:       "default",
			port:            Port{Port: 80},
			expectedJwksURI: "http://xyz",
		},
		{
			hostname:        "jwt-svc-2.default.svc.cluster.local",
			namespace:       "default",
			port:            Port{Port: 80},
			expectedJwksURI: ms.URL + "/oauth2/v3/certs",
		},
	}

	for _, c := range cases {
		ps.ServiceByHostnameAndNamespace[c.hostname] = map[string]*Service{"default": nil}
	}

	for i, c := range cases {
		service := &Service{
			Hostname:   c.hostname,
			Attributes: ServiceAttributes{Namespace: c.namespace},
		}

		// nolint: staticcheck
		if got, _ := ps.AuthenticationPolicyForWorkload(service, &c.port); got.GetOrigins()[0].GetJwt().GetJwksUri() != c.expectedJwksURI {
			t.Errorf("%d. AuthenticationPolicyForWorkload for %s.%s:%v: got(%v) != want(%v)\n", i, c.hostname, c.namespace, c.port, got, c.expectedJwksURI)
		}
	}
}

func TestEnvoyFilters(t *testing.T) {
	proxyVersionRegex := regexp.MustCompile(`1\.4.*`)
	envoyFilters := []*EnvoyFilterWrapper{
		{
			workloadSelector: map[string]string{"app": "v1"},
			Patches: map[networking.EnvoyFilter_ApplyTo][]*EnvoyFilterConfigPatchWrapper{
				networking.EnvoyFilter_LISTENER: {
					{
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{
								ProxyVersion: "1\\.4.*",
							},
						},
						ProxyVersionRegex: proxyVersionRegex,
					},
				},
			},
		},
		{
			workloadSelector: map[string]string{"app": "v1"},
			Patches: map[networking.EnvoyFilter_ApplyTo][]*EnvoyFilterConfigPatchWrapper{
				networking.EnvoyFilter_CLUSTER: {
					{
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{
								ProxyVersion: `1\\.4.*`,
							},
						},
						ProxyVersionRegex: proxyVersionRegex,
					},
				},
			},
		},
	}

	push := &PushContext{
		Mesh: &meshconfig.MeshConfig{
			RootNamespace: "istio-system",
		},
		envoyFiltersByNamespace: map[string][]*EnvoyFilterWrapper{
			"istio-system": envoyFilters,
			"test-ns":      envoyFilters,
		},
	}

	cases := []struct {
		name                    string
		proxy                   *Proxy
		expectedListenerPatches int
		expectedClusterPatches  int
	}{
		{
			name: "proxy matches two envoyfilters",
			proxy: &Proxy{
				Metadata:        &NodeMetadata{IstioVersion: "1.4.0", Labels: map[string]string{"app": "v1"}},
				ConfigNamespace: "test-ns",
			},
			expectedListenerPatches: 2,
			expectedClusterPatches:  2,
		},
		{
			name: "proxy in root namespace matches an envoyfilter",
			proxy: &Proxy{
				Metadata:        &NodeMetadata{IstioVersion: "1.4.0", Labels: map[string]string{"app": "v1"}},
				ConfigNamespace: "istio-system",
			},
			expectedListenerPatches: 1,
			expectedClusterPatches:  1,
		},

		{
			name: "proxy matches no envoyfilter",
			proxy: &Proxy{
				Metadata:        &NodeMetadata{IstioVersion: "1.4.0", Labels: map[string]string{"app": "v2"}},
				ConfigNamespace: "test-ns",
			},
			expectedListenerPatches: 0,
			expectedClusterPatches:  0,
		},

		{
			name: "proxy matches envoyfilter in root ns",
			proxy: &Proxy{
				Metadata:        &NodeMetadata{IstioVersion: "1.4.0", Labels: map[string]string{"app": "v1"}},
				ConfigNamespace: "test-n2",
			},
			expectedListenerPatches: 1,
			expectedClusterPatches:  1,
		},
		{
			name: "proxy version matches no envoyfilters",
			proxy: &Proxy{
				Metadata:        &NodeMetadata{IstioVersion: "1.3.0", Labels: map[string]string{"app": "v1"}},
				ConfigNamespace: "test-ns",
			},
			expectedListenerPatches: 0,
			expectedClusterPatches:  0,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			filter := push.EnvoyFilters(tt.proxy)
			if filter == nil {
				if tt.expectedClusterPatches != 0 || tt.expectedListenerPatches != 0 {
					t.Errorf("Got no envoy filter")
				}
				return
			}
			if len(filter.Patches[networking.EnvoyFilter_CLUSTER]) != tt.expectedClusterPatches {
				t.Errorf("Expect %d envoy filter cluster patches, but got %d", tt.expectedClusterPatches, len(filter.Patches[networking.EnvoyFilter_CLUSTER]))
			}
			if len(filter.Patches[networking.EnvoyFilter_LISTENER]) != tt.expectedListenerPatches {
				t.Errorf("Expect %d envoy filter listener patches, but got %d", tt.expectedListenerPatches, len(filter.Patches[networking.EnvoyFilter_LISTENER]))
			}
		})
	}
}

func TestSidecarScope(t *testing.T) {
	ps := NewPushContext()
	env := &Environment{Watcher: mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})}
	ps.Mesh = env.Mesh()
	ps.ServiceDiscovery = env
	ps.ServiceByHostnameAndNamespace[host.Name("svc1.default.cluster.local")] = map[string]*Service{"default": nil}
	ps.ServiceByHostnameAndNamespace[host.Name("svc2.nosidecar.cluster.local")] = map[string]*Service{"nosidecar": nil}

	configStore := newFakeStore()
	sidecarWithWorkloadSelector := &networking.Sidecar{
		WorkloadSelector: &networking.WorkloadSelector{
			Labels: map[string]string{"app": "foo"},
		},
		Egress: []*networking.IstioEgressListener{
			{
				Hosts: []string{"default/*"},
			},
		},
		OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{},
	}
	sidecarWithoutWorkloadSelector := &networking.Sidecar{
		Egress: []*networking.IstioEgressListener{
			{
				Hosts: []string{"default/*"},
			},
		},
		OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{},
	}
	configWithWorkloadSelector := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Sidecars.Resource().Kind(),
			Group:     collections.IstioNetworkingV1Alpha3Sidecars.Resource().Group(),
			Version:   collections.IstioNetworkingV1Alpha3Sidecars.Resource().Version(),
			Name:      "foo",
			Namespace: "default",
		},
		Spec: sidecarWithWorkloadSelector,
	}
	rootConfig := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Sidecars.Resource().Kind(),
			Group:     collections.IstioNetworkingV1Alpha3Sidecars.Resource().Group(),
			Version:   collections.IstioNetworkingV1Alpha3Sidecars.Resource().Version(),
			Name:      "global",
			Namespace: "istio-system",
		},
		Spec: sidecarWithoutWorkloadSelector,
	}
	_, _ = configStore.Create(configWithWorkloadSelector)
	_, _ = configStore.Create(rootConfig)

	store := istioConfigStore{ConfigStore: configStore}

	env.IstioConfigStore = &store
	if err := ps.initSidecarScopes(env); err != nil {
		t.Fatalf("init sidecar scope failed: %v", err)
	}
	cases := []struct {
		proxy      *Proxy
		collection labels.Collection
		sidecar    string
		describe   string
	}{
		{
			proxy:      &Proxy{ConfigNamespace: "default"},
			collection: labels.Collection{map[string]string{"app": "foo"}},
			sidecar:    "default/foo",
			describe:   "match local sidecar",
		},
		{
			proxy:      &Proxy{ConfigNamespace: "default"},
			collection: labels.Collection{map[string]string{"app": "bar"}},
			sidecar:    "istio-system/global",
			describe:   "no match local sidecar",
		},
		{
			proxy:      &Proxy{ConfigNamespace: "nosidecar"},
			collection: labels.Collection{map[string]string{"app": "bar"}},
			sidecar:    "istio-system/global",
			describe:   "no sidecar",
		},
	}
	for _, c := range cases {
		scope := ps.getSidecarScope(c.proxy, c.collection)
		if c.sidecar != scopeToSidecar(scope) {
			t.Errorf("case with %s should get sidecar %s but got %s", c.describe, c.sidecar, scopeToSidecar(scope))
		}
	}
}

func TestBestEffortInferServiceMTLSMode(t *testing.T) {
	const alphaNamespace string = "alpha-namespace"
	const betaNamespace string = "beta-namespace"
	const otherNamespace string = "other-namespace"
	ps := NewPushContext()
	env := &Environment{Watcher: mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})}
	ps.Mesh = env.Mesh()
	ps.ServiceDiscovery = env
	authNPolicies := map[string]*authn.Policy{
		constants.DefaultAuthenticationPolicyName: {},
		"mtls-strict-svc": {
			Targets: []*authn.TargetSelector{{
				Name: "mtls-strict-svc",
			}},
			Peers: []*authn.PeerAuthenticationMethod{{
				Params: &authn.PeerAuthenticationMethod_Mtls{},
			},
			}},
		"mtls-strict-svc-port": {
			Targets: []*authn.TargetSelector{{
				Name: "mtls-strict-svc-port",
				Ports: []*authn.PortSelector{
					{
						Port: &authn.PortSelector_Number{
							Number: 80,
						},
					},
				},
			}},
			Peers: []*authn.PeerAuthenticationMethod{{
				Params: &authn.PeerAuthenticationMethod_Mtls{},
			},
			}},
		"mtls-disable-svc": {
			Targets: []*authn.TargetSelector{{
				Name: "mtls-disable-svc",
			}},
		},
	}
	configStore := newFakeStore()
	for key, value := range authNPolicies {
		cfg := Config{
			ConfigMeta: ConfigMeta{
				Type:      collections.IstioAuthenticationV1Alpha1Policies.Resource().Kind(),
				Group:     collections.IstioAuthenticationV1Alpha1Policies.Resource().Group(),
				Version:   collections.IstioAuthenticationV1Alpha1Policies.Resource().Version(),
				Name:      key,
				Domain:    "cluster.local",
				Namespace: alphaNamespace,
			},
			Spec: value,
		}
		if _, err := configStore.Create(cfg); err != nil {
			t.Error(err)
		}
	}

	// Add cluster-scoped policy
	globalPolicy := &authn.Policy{
		Peers: []*authn.PeerAuthenticationMethod{{
			Params: &authn.PeerAuthenticationMethod_Mtls{
				Mtls: &authn.MutualTls{
					Mode: authn.MutualTls_PERMISSIVE,
				},
			},
		}},
	}
	globalCfg := Config{
		ConfigMeta: ConfigMeta{
			Type:    collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().Kind(),
			Group:   collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().Group(),
			Version: collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().Version(),
			Name:    constants.DefaultAuthenticationPolicyName,
			Domain:  "cluster.local",
		},
		Spec: globalPolicy,
	}

	// Add beta policies
	configStore.Create(*createTestPeerAuthenticationResource("default", betaNamespace, nil, securityBeta.PeerAuthentication_MutualTLS_STRICT))
	// workload level beta policy.
	configStore.Create(*createTestPeerAuthenticationResource("workload-beta-policy", alphaNamespace, &selectorpb.WorkloadSelector{
		MatchLabels: map[string]string{
			"app":     "httpbin",
			"version": "v1",
		},
	}, securityBeta.PeerAuthentication_MutualTLS_STRICT))

	if _, err := configStore.Create(globalCfg); err != nil {
		t.Error(err)
	}

	store := istioConfigStore{ConfigStore: configStore}
	env.IstioConfigStore = &store
	if err := ps.initAuthnPolicies(env); err != nil {
		t.Fatalf("init authn policies failed: %v", err)
	}

	cases := []struct {
		name             string
		serviceName      host.Name
		serviceNamespace string
		servicePort      int
		wanted           MutualTLSMode
	}{
		{
			name:             "from beta policy",
			serviceName:      "some-service",
			serviceNamespace: betaNamespace,
			servicePort:      80,
			wanted:           MTLSStrict,
		},
		{
			name:             "from alpha global policy",
			serviceName:      "some-service",
			serviceNamespace: otherNamespace,
			servicePort:      80,
			wanted:           MTLSPermissive,
		},
		{
			name:             "from alpha namespace policy",
			serviceName:      "some-service",
			serviceNamespace: alphaNamespace,
			servicePort:      80,
			wanted:           MTLSDisable,
		},
		{
			name:             "from service specific alpha policy",
			serviceName:      "mtls-strict-svc",
			serviceNamespace: alphaNamespace,
			servicePort:      80,
			wanted:           MTLSStrict,
		},
		{
			name:             "from service-port specific alpha policy",
			serviceName:      "mtls-strict-svc-port",
			serviceNamespace: alphaNamespace,
			servicePort:      80,
			wanted:           MTLSStrict,
		},
		{
			name:             "from namespace alpha policy - miss port",
			serviceName:      "mtls-strict-svc-port",
			serviceNamespace: alphaNamespace,
			servicePort:      90,
			wanted:           MTLSDisable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := &Service{
				Hostname:   host.Name(fmt.Sprintf("%s.%s.svc.cluster.local", tc.serviceName, tc.serviceNamespace)),
				Attributes: ServiceAttributes{Namespace: tc.serviceNamespace},
			}
			// Intentionally use the externalService with the same name and namespace for test, though
			// these attributes don't matter.
			externalService := &Service{
				Hostname:     host.Name(fmt.Sprintf("%s.%s.svc.cluster.local", tc.serviceName, tc.serviceNamespace)),
				Attributes:   ServiceAttributes{Namespace: tc.serviceNamespace},
				MeshExternal: true,
			}

			port := &Port{
				Port: tc.servicePort,
			}
			if got := ps.BestEffortInferServiceMTLSMode(service, port); got != tc.wanted {
				t.Fatalf("want %s, but got %s", tc.wanted, got)
			}
			if got := ps.BestEffortInferServiceMTLSMode(externalService, port); got != MTLSUnknown {
				t.Fatalf("MTLS mode for external service should always be %s, but got %s", MTLSUnknown, got)
			}
		})
	}
}

func scopeToSidecar(scope *SidecarScope) string {
	if scope == nil || scope.Config == nil {
		return ""
	}
	return scope.Config.Namespace + "/" + scope.Config.Name
}

type fakeStore struct {
	store map[resource.GroupVersionKind]map[string][]Config
}

func newFakeStore() *fakeStore {
	f := fakeStore{
		store: make(map[resource.GroupVersionKind]map[string][]Config),
	}
	return &f
}

var _ ConfigStore = (*fakeStore)(nil)

func (*fakeStore) Schemas() collection.Schemas {
	return collections.Pilot
}

func (*fakeStore) Get(typ resource.GroupVersionKind, name, namespace string) *Config { return nil }

func (s *fakeStore) List(typ resource.GroupVersionKind, namespace string) ([]Config, error) {
	nsConfigs := s.store[typ]
	if nsConfigs == nil {
		return nil, nil
	}
	var res []Config
	if namespace == NamespaceAll {
		for _, configs := range nsConfigs {
			res = append(res, configs...)
		}
		return res, nil
	}
	return nsConfigs[namespace], nil
}

func (s *fakeStore) Create(config Config) (revision string, err error) {
	configs := s.store[config.GroupVersionKind()]
	if configs == nil {
		configs = make(map[string][]Config)
	}
	configs[config.Namespace] = append(configs[config.Namespace], config)
	s.store[config.GroupVersionKind()] = configs
	return "", nil
}

func (*fakeStore) Update(config Config) (newRevision string, err error) { return "", nil }

func (*fakeStore) Delete(typ resource.GroupVersionKind, name, namespace string) error { return nil }

func (*fakeStore) Version() string {
	return "not implemented"
}
func (*fakeStore) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return "not implemented", nil
}
