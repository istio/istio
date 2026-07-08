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

package xds

import (
	"fmt"
	"testing"

	"istio.io/api/label"
	mesh "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	security "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestProxyNeedsPush(t *testing.T) {
	const (
		svcName        = "svc1.com"
		privateSvcName = "private.com"
		drName         = "dr1"
		vsName         = "vs1"
		scName         = "sc1"
		nsName         = "ns1"
		nsRoot         = "rootns"
		generalName    = "name1"

		invalidNameSuffix = "invalid"
	)

	type Case struct {
		name        string
		proxy       *model.Proxy
		configs     sets.Set[model.ConfigKey]
		forced      bool
		want        bool
		wantConfigs sets.Set[model.ConfigKey]
	}

	sidecar := &model.Proxy{
		Type: model.SidecarProxy, IPAddresses: []string{"127.0.0.1"}, Metadata: &model.NodeMetadata{},
		SidecarScope: &model.SidecarScope{Name: generalName, Namespace: nsName},
	}
	gateway := &model.Proxy{
		Type:            model.Router,
		ConfigNamespace: nsName,
		Metadata:        &model.NodeMetadata{Namespace: nsName},
		Labels:          map[string]string{"gateway": "gateway"},
	}
	// A sidecar-type proxy subscribed to Workload Address resources (e.g. WDS on-demand clients)
	// must keep receiving Address pushes.
	workloadClient := &model.Proxy{
		Type: model.SidecarProxy, IPAddresses: []string{"127.0.0.2"}, Metadata: &model.NodeMetadata{},
		SidecarScope:     &model.SidecarScope{Name: generalName, Namespace: nsName},
		WatchedResources: map[string]*model.WatchedResource{},
	}
	workloadClient.NewWatchedResource(v3.AddressType, nil)

	sidecarScopeKindNames := map[kind.Kind]string{
		kind.ServiceEntry: svcName, kind.VirtualService: vsName, kind.DestinationRule: drName, kind.Sidecar: scName,
	}
	for kind, name := range sidecarScopeKindNames {
		sidecar.SidecarScope.AddConfigDependencies(model.ConfigKey{Kind: kind, Name: name, Namespace: nsName}.HashCode())
	}
	for kind := range UnAffectedConfigKinds[model.SidecarProxy] {
		sidecar.SidecarScope.AddConfigDependencies(model.ConfigKey{
			Kind:      kind,
			Name:      generalName,
			Namespace: nsName,
		}.HashCode())
	}

	cases := []Case{
		{"no namespace or configs", sidecar, nil, false, false, nil},
		{"forced push with no namespace or configs", sidecar, nil, true, true, nil},
		{
			"gateway config for sidecar", sidecar,
			sets.New(model.ConfigKey{Kind: kind.Gateway, Name: generalName, Namespace: nsName}),
			false,
			false,
			sets.New[model.ConfigKey](),
		},
		{
			"gateway config for gateway", gateway,
			sets.New(model.ConfigKey{Kind: kind.Gateway, Name: generalName, Namespace: nsName}),
			false,
			true,
			sets.New(model.ConfigKey{Kind: kind.Gateway, Name: generalName, Namespace: nsName}),
		},
		{
			"sidecar config for gateway", gateway, sets.New(model.ConfigKey{Kind: kind.Sidecar, Name: scName, Namespace: nsName}),
			false,
			false,
			sets.New[model.ConfigKey](),
		},
		{
			"invalid config for sidecar", sidecar,
			sets.New(model.ConfigKey{Kind: kind.Kind(255), Name: generalName, Namespace: nsName}),
			false,
			true,
			sets.New(model.ConfigKey{Kind: kind.Kind(255), Name: generalName, Namespace: nsName}),
		},
		{
			"mixture matched and unmatched config for sidecar",
			sidecar,
			sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: drName, Namespace: nsName},
				model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName + invalidNameSuffix, Namespace: nsName},
			),
			false,
			true,
			sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: drName, Namespace: nsName},
			),
		},
		{
			"mixture unmatched and unmatched config for sidecar",
			sidecar,
			sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: drName + invalidNameSuffix, Namespace: nsName},
				model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName + invalidNameSuffix, Namespace: nsName},
			),
			false,
			false,
			sets.New[model.ConfigKey](),
		},
		{
			"forced push with mixture unmatched and unmatched config for sidecar",
			sidecar,
			sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: drName + invalidNameSuffix, Namespace: nsName},
				model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName + invalidNameSuffix, Namespace: nsName},
			),
			true,
			true,
			sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: drName + invalidNameSuffix, Namespace: nsName},
				model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName + invalidNameSuffix, Namespace: nsName},
			),
		},
		{
			"address config for sidecar", sidecar,
			sets.New(model.ConfigKey{Kind: kind.Address, Name: "Kubernetes//Pod/default/app"}),
			false,
			false,
			sets.New[model.ConfigKey](),
		},
		{
			"address config for workload-subscribed sidecar", workloadClient,
			sets.New(model.ConfigKey{Kind: kind.Address, Name: "Kubernetes//Pod/default/app"}),
			false,
			true,
			sets.New(model.ConfigKey{Kind: kind.Address, Name: "Kubernetes//Pod/default/app"}),
		},
		{
			"address config for gateway", gateway,
			sets.New(model.ConfigKey{Kind: kind.Address, Name: "Kubernetes//Pod/default/app"}),
			false,
			false,
			sets.New[model.ConfigKey](),
		},
		{
			"empty configsUpdated for sidecar",
			sidecar,
			nil,
			false,
			false,
			nil,
		},
		{
			"forced push with empty configsUpdated for sidecar",
			sidecar,
			nil,
			true,
			true,
			nil,
		},
	}

	for k, name := range sidecarScopeKindNames {
		cases = append(cases, Case{ // valid name
			name:        fmt.Sprintf("%s config for sidecar", k.String()),
			proxy:       sidecar,
			configs:     sets.New(model.ConfigKey{Kind: k, Name: name, Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: k, Name: name, Namespace: nsName}),
		}, Case{ // invalid name
			name:        fmt.Sprintf("%s unmatched config for sidecar", k.String()),
			proxy:       sidecar,
			configs:     sets.New(model.ConfigKey{Kind: k, Name: name + invalidNameSuffix, Namespace: nsName}),
			want:        false,
			wantConfigs: sets.New[model.ConfigKey](),
		})
	}

	sidecarNamespaceScopeTypes := []kind.Kind{
		kind.EnvoyFilter, kind.AuthorizationPolicy, kind.RequestAuthentication, kind.WasmPlugin, kind.TrafficExtension,
	}
	for _, k := range sidecarNamespaceScopeTypes {
		cases = append(cases,
			Case{
				name:        fmt.Sprintf("%s config for sidecar in same namespace", k.String()),
				proxy:       sidecar,
				configs:     sets.New(model.ConfigKey{Kind: k, Name: generalName, Namespace: nsName}),
				want:        true,
				wantConfigs: sets.New(model.ConfigKey{Kind: k, Name: generalName, Namespace: nsName}),
			},
			Case{
				name:        fmt.Sprintf("%s config for sidecar in different namespace", k.String()),
				proxy:       sidecar,
				configs:     sets.New(model.ConfigKey{Kind: k, Name: generalName, Namespace: "invalid-namespace"}),
				want:        false,
				wantConfigs: sets.New[model.ConfigKey](),
			},
			Case{
				name:        fmt.Sprintf("%s config in the root namespace", k.String()),
				proxy:       sidecar,
				configs:     sets.New(model.ConfigKey{Kind: k, Name: generalName, Namespace: nsRoot}),
				want:        true,
				wantConfigs: sets.New(model.ConfigKey{Kind: k, Name: generalName, Namespace: nsRoot}),
			},
		)
	}

	// tests for kind-affect-proxy.
	for _, nodeType := range []model.NodeType{model.Router, model.SidecarProxy} {
		proxy := gateway
		if nodeType == model.SidecarProxy {
			proxy = sidecar
		}
		for k := range UnAffectedConfigKinds[proxy.Type] {
			cases = append(cases, Case{
				name:        fmt.Sprintf("kind %s not affect %s", k.String(), nodeType),
				proxy:       proxy,
				configs:     sets.New(model.ConfigKey{Kind: k, Name: generalName + invalidNameSuffix, Namespace: nsName}),
				want:        false,
				wantConfigs: sets.New[model.ConfigKey](),
			})
		}
	}

	// test for gateway proxy dependencies.
	cg := core.NewConfigGenTest(t, core.TestOptions{
		Services: []*model.Service{
			{
				Hostname: svcName,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
			{
				Hostname: privateSvcName,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.None),
					Namespace: nsName,
				},
			},
			{
				Hostname: "foo",
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
		},
	})
	gateway.SetSidecarScope(cg.PushContext())

	// service visibility updated
	cg = core.NewConfigGenTest(t, core.TestOptions{
		Services: []*model.Service{
			{
				Hostname: svcName,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
			{
				Hostname: privateSvcName,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.None),
					Namespace: nsName,
				},
			},
			{
				Hostname: "foo",
				Attributes: model.ServiceAttributes{
					// service visibility changed from public to none
					ExportTo:  sets.New(visibility.None),
					Namespace: nsName,
				},
			},
		},
	})
	gateway.SetSidecarScope(cg.PushContext())

	cases = append(cases,
		Case{
			name:        "service with public visibility for gateway",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName, Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName, Namespace: nsName}),
		},
		Case{
			name:        "service with none visibility for gateway",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: privateSvcName, Namespace: nsName}),
			want:        false,
			wantConfigs: sets.New[model.ConfigKey](),
		},
		Case{
			name:        "service visibility changed from public to none",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "foo", Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "foo", Namespace: nsName}),
		},
	)

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cg.PushContext().Mesh.RootNamespace = nsRoot
			newReq, got := DefaultProxyNeedsPush(tt.proxy, &model.PushRequest{ConfigsUpdated: tt.configs, Push: cg.PushContext(), Forced: tt.forced})
			if got != tt.want {
				t.Fatalf("Got needs push = %v, expected %v", got, tt.want)
			}
			if tt.wantConfigs == nil && newReq.ConfigsUpdated != nil {
				t.Fatalf("Got configs updated = %v, expected none", newReq.ConfigsUpdated)
			}
			if tt.wantConfigs != nil && !tt.wantConfigs.Equals(newReq.ConfigsUpdated) {
				t.Fatalf("Got configs updated = %v, expected %v", newReq.ConfigsUpdated, tt.wantConfigs)
			}
		})
	}

	// test for gateway proxy dependencies with PILOT_FILTER_GATEWAY_CLUSTER_CONFIG enabled.
	test.SetForTest(t, &features.FilterGatewayClusterConfig, true)
	test.SetForTest(t, &features.JwksFetchMode, jwt.Envoy)

	const (
		fooSvc       = "foo"
		extensionSvc = "extension"
		jwksSvc      = "jwks"
	)

	cg = core.NewConfigGenTest(t, core.TestOptions{
		Services: []*model.Service{
			{
				Hostname: fooSvc,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
			{
				Hostname: svcName,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
			{
				Hostname: extensionSvc,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
			{
				Hostname: jwksSvc,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
		},
		Configs: []config.Config{
			{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             svcName,
					Namespace:        nsName,
				},
				Spec: &networking.VirtualService{
					Hosts:    []string{"*"},
					Gateways: []string{generalName},
					Http: []*networking.HTTPRoute{
						{
							Route: []*networking.HTTPRouteDestination{
								{
									Destination: &networking.Destination{
										Host: svcName,
									},
								},
							},
						},
					},
				},
			},
			{
				Meta: config.Meta{
					GroupVersionKind: gvk.RequestAuthentication,
					Name:             jwksSvc,
					Namespace:        nsName,
				},
				Spec: &security.RequestAuthentication{
					Selector: &v1beta1.WorkloadSelector{MatchLabels: gateway.Labels},
					JwtRules: []*security.JWTRule{{JwksUri: "https://" + jwksSvc}},
				},
			},
			{
				Meta: config.Meta{
					GroupVersionKind: gvk.RequestAuthentication,
					Name:             fooSvc,
					Namespace:        nsName,
				},
				Spec: &security.RequestAuthentication{
					// not matching the gateway
					Selector: &v1beta1.WorkloadSelector{MatchLabels: map[string]string{"foo": "bar"}},
					JwtRules: []*security.JWTRule{{JwksUri: "https://" + fooSvc}},
				},
			},
		},
		MeshConfig: &mesh.MeshConfig{
			ExtensionProviders: []*mesh.MeshConfig_ExtensionProvider{
				{
					Provider: &mesh.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp{
						EnvoyExtAuthzHttp: &mesh.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationHttpProvider{
							Service: extensionSvc,
						},
					},
				},
			},
		},
	})

	gateway.MergedGateway = &model.MergedGateway{
		GatewayNameForServer: map[*networking.Server]string{
			{}: nsName + "/" + generalName,
		},
	}
	gateway.SetSidecarScope(cg.PushContext())

	cases = []Case{
		{
			name:        "service without vs attached to gateway",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: fooSvc, Namespace: nsName}),
			want:        false,
			wantConfigs: sets.New[model.ConfigKey](),
		},
		{
			name:        "service with vs attached to gateway",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName, Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName, Namespace: nsName}),
		},
		{
			name:        "mesh config extensions",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: extensionSvc, Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: extensionSvc, Namespace: nsName}),
		},
		{
			name:        "jwks servers",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: jwksSvc, Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: jwksSvc, Namespace: nsName}),
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			newReq, got := DefaultProxyNeedsPush(tt.proxy, &model.PushRequest{ConfigsUpdated: tt.configs, Push: cg.PushContext()})
			if got != tt.want {
				t.Fatalf("Got needs push = %v, expected %v", got, tt.want)
			}
			if tt.wantConfigs == nil && newReq.ConfigsUpdated != nil {
				t.Fatalf("Got configs updated = %v, expected none", newReq.ConfigsUpdated)
			}
			if tt.wantConfigs != nil && !tt.wantConfigs.Equals(newReq.ConfigsUpdated) {
				t.Fatalf("Got configs updated = %v, expected %v", newReq.ConfigsUpdated, tt.wantConfigs)
			}
		})
	}

	gateway.MergedGateway.ContainsAutoPassthroughGateways = true
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			newReq, push := DefaultProxyNeedsPush(tt.proxy, &model.PushRequest{ConfigsUpdated: tt.configs, Push: cg.PushContext()})
			if !push {
				t.Fatalf("Got needs push = %v, expected %v", push, true)
			}
			if !tt.configs.Equals(newReq.ConfigsUpdated) {
				t.Fatalf("Got configs updated = %v, expected %v", newReq.ConfigsUpdated, tt.configs)
			}
		})
	}
}

func TestCheckConnectionIdentity(t *testing.T) {
	cases := []struct {
		name      string
		identity  []string
		sa        string
		namespace string
		success   bool
	}{
		{
			name:      "single match",
			identity:  []string{spiffe.Identity{TrustDomain: "cluster.local", Namespace: "namespace", ServiceAccount: "serviceaccount"}.String()},
			sa:        "serviceaccount",
			namespace: "namespace",
			success:   true,
		},
		{
			name: "second match",
			identity: []string{
				spiffe.Identity{TrustDomain: "cluster.local", Namespace: "bad", ServiceAccount: "serviceaccount"}.String(),
				spiffe.Identity{TrustDomain: "cluster.local", Namespace: "namespace", ServiceAccount: "serviceaccount"}.String(),
			},
			sa:        "serviceaccount",
			namespace: "namespace",
			success:   true,
		},
		{
			name: "no match namespace",
			identity: []string{
				spiffe.Identity{TrustDomain: "cluster.local", Namespace: "bad", ServiceAccount: "serviceaccount"}.String(),
			},
			sa:        "serviceaccount",
			namespace: "namespace",
			success:   false,
		},
		{
			name: "no match service account",
			identity: []string{
				spiffe.Identity{TrustDomain: "cluster.local", Namespace: "namespace", ServiceAccount: "bad"}.String(),
			},
			sa:        "serviceaccount",
			namespace: "namespace",
			success:   false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &model.Proxy{ConfigNamespace: tt.namespace, Metadata: &model.NodeMetadata{ServiceAccount: tt.sa}}
			if _, err := checkConnectionIdentity(proxy, tt.identity); (err == nil) != tt.success {
				t.Fatalf("expected success=%v, got err=%v", tt.success, err)
			}
		})
	}
}

func TestWaypointNeedsPush(t *testing.T) {
	const (
		waypointHost = "waypoint.default.svc.cluster.local"
		waypointVIP  = "3.0.0.0"
	)
	waypoint := &model.Proxy{
		Type:            model.Waypoint,
		ConfigNamespace: "default",
		Metadata:        &model.NodeMetadata{ClusterID: "c1", Network: "net1"},
		ServiceTargets: []model.ServiceTarget{{
			Service: &model.Service{
				Hostname:    waypointHost,
				ClusterVIPs: model.AddressMap{Addresses: map[cluster.ID][]string{"c1": {waypointVIP}}},
			},
		}},
	}
	eastwest := &model.Proxy{
		Type:            model.Waypoint,
		ConfigNamespace: "default",
		Metadata:        &model.NodeMetadata{ClusterID: "c1", Network: "net1"},
		Labels: map[string]string{
			label.GatewayManaged.Name: constants.ManagedGatewayEastWestControllerLabel,
		},
	}

	addressUpdate := func(refs ...model.WaypointReference) *model.PushRequest {
		return &model.PushRequest{
			ConfigsUpdated:   sets.New(model.ConfigKey{Kind: kind.Address, Name: "Kubernetes//Pod/default/app"}),
			WaypointsUpdated: sets.New(refs...),
		}
	}

	cases := []struct {
		name  string
		proxy *model.Proxy
		req   *model.PushRequest
		want  bool
	}{
		{
			name:  "no address updates",
			proxy: waypoint,
			req:   &model.PushRequest{ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "svc1.com", Namespace: "default"})},
			want:  false,
		},
		{
			// Ordinary pod churn: addresses changed but none of them attached to a waypoint
			name:  "address update without waypoint references",
			proxy: waypoint,
			req:   addressUpdate(),
			want:  false,
		},
		{
			name:  "address update attached to this waypoint by hostname",
			proxy: waypoint,
			req:   addressUpdate(model.WaypointReference{Namespace: "default", Hostname: waypointHost}),
			want:  true,
		},
		{
			name:  "address update attached to another waypoint",
			proxy: waypoint,
			req:   addressUpdate(model.WaypointReference{Namespace: "other", Hostname: "waypoint.other.svc.cluster.local"}),
			want:  false,
		},
		{
			name:  "address update attached to this waypoint by address",
			proxy: waypoint,
			req:   addressUpdate(model.WaypointReference{Network: "net1", Address: waypointVIP}),
			want:  true,
		},
		{
			name:  "address update attached to the same address on another network",
			proxy: waypoint,
			req:   addressUpdate(model.WaypointReference{Network: "net2", Address: waypointVIP}),
			want:  false,
		},
		{
			// East-west gateways serve global services rather than attached ones, so they
			// cannot be scoped by attachment
			name:  "east-west gateway",
			proxy: eastwest,
			req:   addressUpdate(),
			want:  true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, waypointNeedsPush(tt.req, tt.proxy), tt.want)
		})
	}

	t.Run("scoping disabled", func(t *testing.T) {
		test.SetForTest(t, &features.ScopedAddressPushes, false)
		assert.Equal(t, waypointNeedsPush(addressUpdate(), waypoint), true)
	})
}
