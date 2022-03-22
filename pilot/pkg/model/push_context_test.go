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
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	"go.uber.org/atomic"

	extensions "istio.io/api/extensions/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	securityBeta "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
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
				Full:  true,
				Push:  push0,
				Start: t0,
				ConfigsUpdated: map[ConfigKey]struct{}{
					{Kind: config.GroupVersionKind{Kind: "cfg1"}, Namespace: "ns1"}: {},
				},
				Reason: []TriggerReason{ServiceUpdate, ServiceUpdate},
			},
			&PushRequest{
				Full:  false,
				Push:  push1,
				Start: t1,
				ConfigsUpdated: map[ConfigKey]struct{}{
					{Kind: config.GroupVersionKind{Kind: "cfg2"}, Namespace: "ns2"}: {},
				},
				Reason: []TriggerReason{EndpointUpdate},
			},
			PushRequest{
				Full:  true,
				Push:  push1,
				Start: t0,
				ConfigsUpdated: map[ConfigKey]struct{}{
					{Kind: config.GroupVersionKind{Kind: "cfg1"}, Namespace: "ns1"}: {},
					{Kind: config.GroupVersionKind{Kind: "cfg2"}, Namespace: "ns2"}: {},
				},
				Reason: []TriggerReason{ServiceUpdate, ServiceUpdate, EndpointUpdate},
			},
		},
		{
			"skip config type merge: one empty",
			&PushRequest{Full: true, ConfigsUpdated: nil},
			&PushRequest{Full: true, ConfigsUpdated: map[ConfigKey]struct{}{{
				Kind: config.GroupVersionKind{Kind: "cfg2"},
			}: {}}},
			PushRequest{Full: true, ConfigsUpdated: nil, Reason: nil},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.left.CopyMerge(tt.right)
			if !reflect.DeepEqual(&tt.merged, got) {
				t.Fatalf("expected %v, got %v", &tt.merged, got)
			}
			got = tt.left.Merge(tt.right)
			if !reflect.DeepEqual(&tt.merged, got) {
				t.Fatalf("expected %v, got %v", &tt.merged, got)
			}
		})
	}
}

func TestConcurrentMerge(t *testing.T) {
	reqA := &PushRequest{Reason: make([]TriggerReason, 0, 100)}
	reqB := &PushRequest{Reason: []TriggerReason{ServiceUpdate, ProxyUpdate}}
	for i := 0; i < 50; i++ {
		go func() {
			reqA.CopyMerge(reqB)
		}()
	}
	if len(reqA.Reason) != 0 {
		t.Fatalf("reqA modified: %v", reqA.Reason)
	}
	if len(reqB.Reason) != 2 {
		t.Fatalf("reqB modified: %v", reqB.Reason)
	}
}

func TestEnvoyFilters(t *testing.T) {
	proxyVersionRegex := regexp.MustCompile(`1\.4.*`)
	envoyFilters := []*EnvoyFilterWrapper{
		{
			Name:             "ef1",
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
			Name:             "ef2",
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

	if !push.HasEnvoyFilters("ef1", "test-ns") {
		t.Errorf("Check presence of EnvoyFilter ef1 at test-ns got false, want true")
	}
	if push.HasEnvoyFilters("ef3", "test-ns") {
		t.Errorf("Check presence of EnvoyFilter ef3 at test-ns got true, want false")
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

func TestEnvoyFilterOrder(t *testing.T) {
	env := &Environment{}
	store := istioConfigStore{ConfigStore: NewFakeStore()}

	ctime := time.Now()

	envoyFilters := []config.Config{
		{
			Meta: config.Meta{Name: "default-priority", Namespace: "testns-1", GroupVersionKind: gvk.EnvoyFilter},
			Spec: &networking.EnvoyFilter{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						Patch: &networking.EnvoyFilter_Patch{},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
			},
		},
		{
			Meta: config.Meta{Name: "default-priority", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter},
			Spec: &networking.EnvoyFilter{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						Patch: &networking.EnvoyFilter_Patch{},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
			},
		},
		{
			Meta: config.Meta{Name: "b-medium-priority", Namespace: "testns-1", GroupVersionKind: gvk.EnvoyFilter, CreationTimestamp: ctime},
			Spec: &networking.EnvoyFilter{
				Priority: 10,
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						Patch: &networking.EnvoyFilter_Patch{},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
			},
		},
		{
			Meta: config.Meta{Name: "a-medium-priority", Namespace: "testns-1", GroupVersionKind: gvk.EnvoyFilter, CreationTimestamp: ctime},
			Spec: &networking.EnvoyFilter{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						Patch: &networking.EnvoyFilter_Patch{},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
			},
		},
		{
			Meta: config.Meta{Name: "b-medium-priority", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter, CreationTimestamp: ctime},
			Spec: &networking.EnvoyFilter{
				Priority: 10,
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						Patch: &networking.EnvoyFilter_Patch{},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
			},
		},
		{
			Meta: config.Meta{Name: "a-medium-priority", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter, CreationTimestamp: ctime},
			Spec: &networking.EnvoyFilter{
				Priority: 10,
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						Patch: &networking.EnvoyFilter_Patch{},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
			},
		},
		{
			Meta: config.Meta{Name: "a-low-priority", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter, CreationTimestamp: time.Now()},
			Spec: &networking.EnvoyFilter{
				Priority: 20,
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						Patch: &networking.EnvoyFilter_Patch{},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
			},
		},
		{
			Meta: config.Meta{Name: "b-low-priority", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter, CreationTimestamp: ctime},
			Spec: &networking.EnvoyFilter{
				Priority: 20,
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						Patch: &networking.EnvoyFilter_Patch{},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
			},
		},
		{
			Meta: config.Meta{Name: "high-priority", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter},
			Spec: &networking.EnvoyFilter{
				Priority: -1,
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						Patch: &networking.EnvoyFilter_Patch{},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
			},
		},
		{
			Meta: config.Meta{Name: "super-high-priority", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter},
			Spec: &networking.EnvoyFilter{
				Priority: -10,
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						Patch: &networking.EnvoyFilter_Patch{},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
			},
		},
	}

	expectedns := []string{
		"testns/super-high-priority", "testns/high-priority", "testns/default-priority", "testns/a-medium-priority",
		"testns/b-medium-priority", "testns/b-low-priority", "testns/a-low-priority",
	}

	expectedns1 := []string{"testns-1/default-priority", "testns-1/a-medium-priority", "testns-1/b-medium-priority"}

	for _, cfg := range envoyFilters {
		_, _ = store.Create(cfg)
	}
	env.IstioConfigStore = &store
	m := mesh.DefaultMeshConfig()
	env.Watcher = mesh.NewFixedWatcher(m)
	env.Init()

	// Init a new push context
	pc := NewPushContext()
	if err := pc.initEnvoyFilters(env); err != nil {
		t.Fatal(err)
	}
	gotns := make([]string, 0)
	for _, filter := range pc.envoyFiltersByNamespace["testns"] {
		gotns = append(gotns, filter.Keys()...)
	}
	gotns1 := make([]string, 0)
	for _, filter := range pc.envoyFiltersByNamespace["testns-1"] {
		gotns1 = append(gotns1, filter.Keys()...)
	}
	if !reflect.DeepEqual(expectedns, gotns) {
		t.Errorf("Envoy filters are not ordered as expected. expected: %v got: %v", expectedns, gotns)
	}
	if !reflect.DeepEqual(expectedns1, gotns1) {
		t.Errorf("Envoy filters are not ordered as expected. expected: %v got: %v", expectedns1, gotns1)
	}
}

func TestWasmPlugins(t *testing.T) {
	env := &Environment{}
	store := istioConfigStore{ConfigStore: NewFakeStore()}

	wasmPlugins := map[string]*config.Config{
		"invalid-type": {
			Meta: config.Meta{Name: "invalid-type", Namespace: constants.IstioSystemNamespace, GroupVersionKind: gvk.WasmPlugin},
			Spec: &networking.DestinationRule{},
		},
		"invalid-url": {
			Meta: config.Meta{Name: "invalid-url", Namespace: constants.IstioSystemNamespace, GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHN,
				Priority: &types.Int64Value{Value: 5},
				Url:      "notavalid%%Url;",
			},
		},
		"authn-low-prio-all": {
			Meta: config.Meta{Name: "authn-low-prio-all", Namespace: "testns-1", GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHN,
				Priority: &types.Int64Value{Value: 10},
				Url:      "file:///etc/istio/filters/authn.wasm",
				PluginConfig: &types.Struct{
					Fields: map[string]*types.Value{
						"test": {
							Kind: &types.Value_StringValue{StringValue: "test"},
						},
					},
				},
				Sha256: "f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2",
			},
		},
		"global-authn-low-prio-ingress": {
			Meta: config.Meta{Name: "global-authn-low-prio-ingress", Namespace: constants.IstioSystemNamespace, GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHN,
				Priority: &types.Int64Value{Value: 5},
				Selector: &selectorpb.WorkloadSelector{
					MatchLabels: map[string]string{
						"istio": "ingressgateway",
					},
				},
			},
		},
		"authn-med-prio-all": {
			Meta: config.Meta{Name: "authn-med-prio-all", Namespace: "testns-1", GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHN,
				Priority: &types.Int64Value{Value: 50},
			},
		},
		"global-authn-high-prio-app": {
			Meta: config.Meta{Name: "global-authn-high-prio-app", Namespace: constants.IstioSystemNamespace, GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHN,
				Priority: &types.Int64Value{Value: 1000},
				Selector: &selectorpb.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "productpage",
					},
				},
			},
		},
		"global-authz-med-prio-app": {
			Meta: config.Meta{Name: "global-authz-med-prio-app", Namespace: constants.IstioSystemNamespace, GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHZ,
				Priority: &types.Int64Value{Value: 50},
				Selector: &selectorpb.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "productpage",
					},
				},
			},
		},
		"authz-high-prio-ingress": {
			Meta: config.Meta{Name: "authz-high-prio-ingress", Namespace: "testns-2", GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHZ,
				Priority: &types.Int64Value{Value: 1000},
			},
		},
	}

	testCases := []struct {
		name               string
		node               *Proxy
		expectedExtensions map[extensions.PluginPhase][]*WasmPluginWrapper
	}{
		{
			name:               "nil proxy",
			node:               nil,
			expectedExtensions: nil,
		},
		{
			name: "nomatch",
			node: &Proxy{
				ConfigNamespace: "other",
			},
			expectedExtensions: map[extensions.PluginPhase][]*WasmPluginWrapper{},
		},
		{
			name: "ingress",
			node: &Proxy{
				ConfigNamespace: "other",
				Metadata: &NodeMetadata{
					Labels: map[string]string{
						"istio": "ingressgateway",
					},
				},
			},
			expectedExtensions: map[extensions.PluginPhase][]*WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					convertToWasmPluginWrapper(wasmPlugins["global-authn-low-prio-ingress"]),
				},
			},
		},
		{
			name: "ingress-testns-1",
			node: &Proxy{
				ConfigNamespace: "testns-1",
				Metadata: &NodeMetadata{
					Labels: map[string]string{
						"istio": "ingressgateway",
					},
				},
			},
			expectedExtensions: map[extensions.PluginPhase][]*WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					convertToWasmPluginWrapper(wasmPlugins["authn-med-prio-all"]),
					convertToWasmPluginWrapper(wasmPlugins["authn-low-prio-all"]),
					convertToWasmPluginWrapper(wasmPlugins["global-authn-low-prio-ingress"]),
				},
			},
		},
		{
			name: "testns-2",
			node: &Proxy{
				ConfigNamespace: "testns-2",
				Metadata: &NodeMetadata{
					Labels: map[string]string{
						"app": "productpage",
					},
				},
			},
			expectedExtensions: map[extensions.PluginPhase][]*WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					convertToWasmPluginWrapper(wasmPlugins["global-authn-high-prio-app"]),
				},
				extensions.PluginPhase_AUTHZ: {
					convertToWasmPluginWrapper(wasmPlugins["authz-high-prio-ingress"]),
					convertToWasmPluginWrapper(wasmPlugins["global-authz-med-prio-app"]),
				},
			},
		},
	}

	for _, config := range wasmPlugins {
		store.Create(*config)
	}
	env.IstioConfigStore = &store
	m := mesh.DefaultMeshConfig()
	env.Watcher = mesh.NewFixedWatcher(m)
	env.Init()

	// Init a new push context
	pc := NewPushContext()
	pc.Mesh = m
	if err := pc.initWasmPlugins(env); err != nil {
		t.Fatal(err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := pc.WasmPlugins(tc.node)
			if !reflect.DeepEqual(tc.expectedExtensions, result) {
				t.Errorf("WasmPlugins did not match expectations\n\ngot: %v\n\nexpected: %v", result, tc.expectedExtensions)
			}
		})
	}
}

func TestServiceIndex(t *testing.T) {
	g := NewWithT(t)
	env := &Environment{}
	store := istioConfigStore{ConfigStore: NewFakeStore()}

	env.IstioConfigStore = &store
	env.ServiceDiscovery = &localServiceDiscovery{
		services: []*Service{
			{
				Hostname: "svc-unset",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Namespace: "test1",
				},
			},
			{
				Hostname: "svc-public",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Namespace: "test1",
					ExportTo:  map[visibility.Instance]bool{visibility.Public: true},
				},
			},
			{
				Hostname: "svc-private",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Namespace: "test1",
					ExportTo:  map[visibility.Instance]bool{visibility.Private: true},
				},
			},
			{
				Hostname: "svc-none",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Namespace: "test1",
					ExportTo:  map[visibility.Instance]bool{visibility.None: true},
				},
			},
			{
				Hostname: "svc-namespace",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Namespace: "test1",
					ExportTo:  map[visibility.Instance]bool{"namespace": true},
				},
			},
		},
		serviceInstances: []*ServiceInstance{{
			Endpoint: &IstioEndpoint{
				Address:      "192.168.1.2",
				EndpointPort: 8000,
				TLSMode:      DisabledTLSModeLabel,
			},
		}},
	}
	m := mesh.DefaultMeshConfig()
	env.Watcher = mesh.NewFixedWatcher(m)
	env.Init()

	// Init a new push context
	pc := NewPushContext()
	if err := pc.InitContext(env, nil, nil); err != nil {
		t.Fatal(err)
	}
	si := pc.ServiceIndex

	// Should have all 5 services
	g.Expect(si.instancesByPort).To(HaveLen(5))
	g.Expect(si.HostnameAndNamespace).To(HaveLen(5))

	// Should just have "namespace"
	g.Expect(si.exportedToNamespace).To(HaveLen(1))
	g.Expect(serviceNames(si.exportedToNamespace["namespace"])).To(Equal([]string{"svc-namespace"}))

	g.Expect(serviceNames(si.public)).To(Equal([]string{"svc-public", "svc-unset"}))

	// Should just have "test1"
	g.Expect(si.privateByNamespace).To(HaveLen(1))
	g.Expect(serviceNames(si.privateByNamespace["test1"])).To(Equal([]string{"svc-private"}))
}

func TestIsServiceVisible(t *testing.T) {
	targetNamespace := "foo"
	cases := []struct {
		name        string
		pushContext *PushContext
		service     *Service
		expect      bool
	}{
		{
			name: "service whose namespace is foo has no exportTo map with global private",
			pushContext: &PushContext{
				exportToDefaults: exportToDefaults{
					service: map[visibility.Instance]bool{
						visibility.Private: true,
					},
				},
			},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "foo",
				},
			},
			expect: true,
		},
		{
			name: "service whose namespace is bar has no exportTo map with global private",
			pushContext: &PushContext{
				exportToDefaults: exportToDefaults{
					service: map[visibility.Instance]bool{
						visibility.Private: true,
					},
				},
			},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "bar",
				},
			},
			expect: false,
		},
		{
			name: "service whose namespace is bar has no exportTo map with global public",
			pushContext: &PushContext{
				exportToDefaults: exportToDefaults{
					service: map[visibility.Instance]bool{
						visibility.Public: true,
					},
				},
			},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "bar",
				},
			},
			expect: true,
		},
		{
			name:        "service whose namespace is foo has exportTo map with private",
			pushContext: &PushContext{},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "foo",
					ExportTo: map[visibility.Instance]bool{
						visibility.Private: true,
					},
				},
			},
			expect: true,
		},
		{
			name:        "service whose namespace is bar has exportTo map with private",
			pushContext: &PushContext{},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "bar",
					ExportTo: map[visibility.Instance]bool{
						visibility.Private: true,
					},
				},
			},
			expect: false,
		},
		{
			name:        "service whose namespace is bar has exportTo map with public",
			pushContext: &PushContext{},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "bar",
					ExportTo: map[visibility.Instance]bool{
						visibility.Public: true,
					},
				},
			},
			expect: true,
		},
		{
			name:        "service whose namespace is bar has exportTo map with specific namespace foo",
			pushContext: &PushContext{},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "bar",
					ExportTo: map[visibility.Instance]bool{
						visibility.Instance("foo"): true,
					},
				},
			},
			expect: true,
		},
		{
			name:        "service whose namespace is bar has exportTo map with specific namespace baz",
			pushContext: &PushContext{},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "bar",
					ExportTo: map[visibility.Instance]bool{
						visibility.Instance("baz"): true,
					},
				},
			},
			expect: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isVisible := c.pushContext.IsServiceVisible(c.service, targetNamespace)

			g := NewWithT(t)
			g.Expect(isVisible).To(Equal(c.expect))
		})
	}
}

func serviceNames(svcs []*Service) []string {
	var s []string
	for _, ss := range svcs {
		s = append(s, string(ss.Hostname))
	}
	sort.Strings(s)
	return s
}

func TestInitPushContext(t *testing.T) {
	env := &Environment{}
	configStore := NewFakeStore()
	_, _ = configStore.Create(config.Config{
		Meta: config.Meta{
			Name:             "rule1",
			Namespace:        "test1",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"rule1.com"},
			ExportTo: []string{".", "ns1"},
		},
	})
	_, _ = configStore.Create(config.Config{
		Meta: config.Meta{
			Name:             "rule1",
			Namespace:        "test1",
			GroupVersionKind: gvk.DestinationRule,
		},
		Spec: &networking.DestinationRule{
			ExportTo: []string{".", "ns1"},
		},
	})
	store := istioConfigStore{ConfigStore: configStore}

	env.IstioConfigStore = &store
	env.ServiceDiscovery = &localServiceDiscovery{
		services: []*Service{
			{
				Hostname: "svc1",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Namespace: "test1",
				},
			},
			{
				Hostname: "svc2",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Namespace: "test1",
					ExportTo:  map[visibility.Instance]bool{visibility.Public: true},
				},
			},
		},
		serviceInstances: []*ServiceInstance{{
			Endpoint: &IstioEndpoint{
				Address:      "192.168.1.2",
				EndpointPort: 8000,
				TLSMode:      DisabledTLSModeLabel,
			},
		}},
	}
	m := mesh.DefaultMeshConfig()
	env.Watcher = mesh.NewFixedWatcher(m)
	env.Init()

	// Init a new push context
	old := NewPushContext()
	if err := old.InitContext(env, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Create a new one, copying from the old one
	// Pass a ConfigsUpdated otherwise we would just copy it directly
	newPush := NewPushContext()
	if err := newPush.InitContext(env, old, &PushRequest{
		ConfigsUpdated: map[ConfigKey]struct{}{
			{Kind: gvk.Secret}: {},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Check to ensure the update is identical to the old one
	// There is probably a better way to do this.
	diff := cmp.Diff(old, newPush,
		// Allow looking into exported fields for parts of push context
		cmp.AllowUnexported(PushContext{}, exportToDefaults{}, serviceIndex{}, virtualServiceIndex{},
			destinationRuleIndex{}, gatewayIndex{}, processedDestRules{}, IstioEgressListenerWrapper{}, SidecarScope{},
			AuthenticationPolicies{}, NetworkManager{}, sidecarIndex{}, Telemetries{}, ProxyConfigs{}),
		// These are not feasible/worth comparing
		cmpopts.IgnoreTypes(sync.RWMutex{}, localServiceDiscovery{}, FakeStore{}, atomic.Bool{}, sync.Mutex{}),
		cmpopts.IgnoreInterfaces(struct{ mesh.Holder }{}),
	)
	if diff != "" {
		t.Fatalf("Push context had a diff after update: %v", diff)
	}
}

func TestSidecarScope(t *testing.T) {
	ps := NewPushContext()
	env := &Environment{Watcher: mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})}
	ps.Mesh = env.Mesh()
	ps.ServiceIndex.HostnameAndNamespace["svc1.default.cluster.local"] = map[string]*Service{"default": nil}
	ps.ServiceIndex.HostnameAndNamespace["svc2.nosidecar.cluster.local"] = map[string]*Service{"nosidecar": nil}
	ps.ServiceIndex.HostnameAndNamespace["svc3.istio-system.cluster.local"] = map[string]*Service{"istio-system": nil}

	configStore := NewFakeStore()
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
	configWithWorkloadSelector := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind(),
			Name:             "foo",
			Namespace:        "default",
		},
		Spec: sidecarWithWorkloadSelector,
	}
	rootConfig := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind(),
			Name:             "global",
			Namespace:        constants.IstioSystemNamespace,
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
			proxy:      &Proxy{Type: SidecarProxy, ConfigNamespace: "default"},
			collection: labels.Collection{map[string]string{"app": "foo"}},
			sidecar:    "default/foo",
			describe:   "match local sidecar",
		},
		{
			proxy:      &Proxy{Type: SidecarProxy, ConfigNamespace: "default"},
			collection: labels.Collection{map[string]string{"app": "bar"}},
			sidecar:    "default/global",
			describe:   "no match local sidecar",
		},
		{
			proxy:      &Proxy{Type: SidecarProxy, ConfigNamespace: "nosidecar"},
			collection: labels.Collection{map[string]string{"app": "bar"}},
			sidecar:    "nosidecar/global",
			describe:   "no sidecar",
		},
		{
			proxy:      &Proxy{Type: Router, ConfigNamespace: "istio-system"},
			collection: labels.Collection{map[string]string{"app": "istio-gateway"}},
			sidecar:    "istio-system/default-sidecar",
			describe:   "gateway sidecar scope",
		},
	}
	for _, c := range cases {
		t.Run(c.describe, func(t *testing.T) {
			scope := ps.getSidecarScope(c.proxy, c.collection)
			if c.sidecar != scopeToSidecar(scope) {
				t.Errorf("should get sidecar %s but got %s", c.sidecar, scopeToSidecar(scope))
			}
		})
	}
}

func TestBestEffortInferServiceMTLSMode(t *testing.T) {
	const partialNS string = "partial"
	const wholeNS string = "whole"
	ps := NewPushContext()
	env := &Environment{Watcher: mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})}
	sd := &localServiceDiscovery{}
	env.ServiceDiscovery = sd
	ps.Mesh = env.Mesh()

	configStore := NewFakeStore()

	// Add beta policies
	_, _ = configStore.Create(*createTestPeerAuthenticationResource("default", wholeNS, time.Now(), nil, securityBeta.PeerAuthentication_MutualTLS_STRICT))
	// workload level beta policy.
	_, _ = configStore.Create(*createTestPeerAuthenticationResource("workload-beta-policy", partialNS, time.Now(), &selectorpb.WorkloadSelector{
		MatchLabels: map[string]string{
			"app":     "httpbin",
			"version": "v1",
		},
	}, securityBeta.PeerAuthentication_MutualTLS_DISABLE))

	store := istioConfigStore{ConfigStore: configStore}
	env.IstioConfigStore = &store
	if err := ps.initAuthnPolicies(env); err != nil {
		t.Fatalf("init authn policies failed: %v", err)
	}

	instancePlainText := &ServiceInstance{
		Endpoint: &IstioEndpoint{
			Address:      "192.168.1.2",
			EndpointPort: 1000000,
			TLSMode:      DisabledTLSModeLabel,
		},
	}

	cases := []struct {
		name              string
		serviceNamespace  string
		servicePort       int
		serviceResolution Resolution
		serviceInstances  []*ServiceInstance
		wanted            MutualTLSMode
	}{
		{
			name:              "from namespace policy",
			serviceNamespace:  wholeNS,
			servicePort:       80,
			serviceResolution: ClientSideLB,
			wanted:            MTLSStrict,
		},
		{
			name:              "from mesh default",
			serviceNamespace:  partialNS,
			servicePort:       80,
			serviceResolution: ClientSideLB,
			wanted:            MTLSPermissive,
		},
		{
			name:              "headless service with no instances found yet",
			serviceNamespace:  partialNS,
			servicePort:       80,
			serviceResolution: Passthrough,
			wanted:            MTLSDisable,
		},
		{
			name:              "headless service with instances",
			serviceNamespace:  partialNS,
			servicePort:       80,
			serviceResolution: Passthrough,
			serviceInstances:  []*ServiceInstance{instancePlainText},
			wanted:            MTLSDisable,
		},
	}

	serviceName := host.Name("some-service")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := &Service{
				Hostname:   host.Name(fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, tc.serviceNamespace)),
				Resolution: tc.serviceResolution,
				Attributes: ServiceAttributes{Namespace: tc.serviceNamespace},
			}
			// Intentionally use the externalService with the same name and namespace for test, though
			// these attributes don't matter.
			externalService := &Service{
				Hostname:     host.Name(fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, tc.serviceNamespace)),
				Resolution:   tc.serviceResolution,
				Attributes:   ServiceAttributes{Namespace: tc.serviceNamespace},
				MeshExternal: true,
			}

			sd.serviceInstances = tc.serviceInstances
			port := &Port{
				Port: tc.servicePort,
			}
			if got := ps.BestEffortInferServiceMTLSMode(nil, service, port); got != tc.wanted {
				t.Fatalf("want %s, but got %s", tc.wanted, got)
			}
			if got := ps.BestEffortInferServiceMTLSMode(nil, externalService, port); got != MTLSUnknown {
				t.Fatalf("MTLS mode for external service should always be %s, but got %s", MTLSUnknown, got)
			}
		})
	}
}

func scopeToSidecar(scope *SidecarScope) string {
	if scope == nil {
		return ""
	}
	return scope.Namespace + "/" + scope.Name
}

func TestSetDestinationRuleInheritance(t *testing.T) {
	features.EnableDestinationRuleInheritance = true
	defer func() {
		features.EnableDestinationRuleInheritance = false
	}()

	ps := NewPushContext()
	ps.Mesh = &meshconfig.MeshConfig{RootNamespace: "istio-system"}
	testhost := "httpbin.org"
	meshDestinationRule := config.Config{
		Meta: config.Meta{
			Name:      "meshRule",
			Namespace: ps.Mesh.RootNamespace,
		},
		Spec: &networking.DestinationRule{
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &types.Duration{Seconds: 1},
						MaxConnections: 111,
					},
				},
				Tls: &networking.ClientTLSSettings{
					Mode:              networking.ClientTLSSettings_MUTUAL,
					ClientCertificate: "/etc/certs/myclientcert.pem",
					PrivateKey:        "/etc/certs/client_private_key.pem",
					CaCertificates:    "/etc/certs/rootcacerts.pem",
				},
			},
		},
	}
	nsDestinationRule := config.Config{
		Meta: config.Meta{
			Name:      "nsRule",
			Namespace: "test",
		},
		Spec: &networking.DestinationRule{
			TrafficPolicy: &networking.TrafficPolicy{
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveGatewayErrors: &types.UInt32Value{Value: 222},
					Interval:                 &types.Duration{Seconds: 22},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 2,
					},
				},
			},
		},
	}
	svcDestinationRule := config.Config{
		Meta: config.Meta{
			Name:      "svcRule",
			Namespace: "test",
		},
		Spec: &networking.DestinationRule{
			Host: testhost,
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &types.Duration{Seconds: 33},
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					Consecutive_5XxErrors: &types.UInt32Value{Value: 3},
				},
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
			},
		},
	}
	destinationRuleNamespace2 := config.Config{
		Meta: config.Meta{
			Name:      "svcRule2",
			Namespace: "test2",
		},
		Spec: &networking.DestinationRule{
			Host: testhost,
			Subsets: []*networking.Subset{
				{
					Name: "subset1",
				},
				{
					Name: "subset2",
				},
			},
		},
	}

	testCases := []struct {
		name            string
		proxyNs         string
		serviceNs       string
		serviceHostname string
		expectedConfig  string
		expectedPolicy  *networking.TrafficPolicy
	}{
		{
			name:            "merge mesh+namespace+service DR",
			proxyNs:         "test",
			serviceNs:       "test",
			serviceHostname: testhost,
			expectedConfig:  "svcRule",
			expectedPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &types.Duration{Seconds: 33},
						MaxConnections: 111,
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					Consecutive_5XxErrors:    &types.UInt32Value{Value: 3},
					ConsecutiveGatewayErrors: &types.UInt32Value{Value: 222},
					Interval:                 &types.Duration{Seconds: 22},
				},
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
			},
		},
		{
			name:            "merge mesh+service DR",
			proxyNs:         "test2",
			serviceNs:       "test2",
			serviceHostname: testhost,
			expectedConfig:  "svcRule2",
			expectedPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &types.Duration{Seconds: 1},
						MaxConnections: 111,
					},
				},
				Tls: &networking.ClientTLSSettings{
					Mode:              networking.ClientTLSSettings_MUTUAL,
					ClientCertificate: "/etc/certs/myclientcert.pem",
					PrivateKey:        "/etc/certs/client_private_key.pem",
					CaCertificates:    "/etc/certs/rootcacerts.pem",
				},
			},
		},
		{
			name:            "unknown host returns merged mesh+namespace",
			proxyNs:         "test",
			serviceNs:       "test",
			serviceHostname: "unknown.host",
			expectedConfig:  "nsRule",
			expectedPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 2,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &types.Duration{Seconds: 1},
						MaxConnections: 111,
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveGatewayErrors: &types.UInt32Value{Value: 222},
					Interval:                 &types.Duration{Seconds: 22},
				},
				Tls: &networking.ClientTLSSettings{
					Mode:              networking.ClientTLSSettings_MUTUAL,
					ClientCertificate: "/etc/certs/myclientcert.pem",
					PrivateKey:        "/etc/certs/client_private_key.pem",
					CaCertificates:    "/etc/certs/rootcacerts.pem",
				},
			},
		},
		{
			name:            "unknwn namespace+host returns mesh",
			proxyNs:         "unknown",
			serviceNs:       "unknown",
			serviceHostname: "unknown.host",
			expectedConfig:  "meshRule",
			expectedPolicy:  meshDestinationRule.Spec.(*networking.DestinationRule).TrafficPolicy,
		},
	}

	ps.SetDestinationRules([]config.Config{meshDestinationRule, nsDestinationRule, svcDestinationRule, destinationRuleNamespace2})

	for _, tt := range testCases {
		mergedConfig := ps.destinationRule(tt.proxyNs,
			&Service{
				Hostname: host.Name(tt.serviceHostname),
				Attributes: ServiceAttributes{
					Namespace: tt.serviceNs,
				},
			})
		if mergedConfig.Name != tt.expectedConfig {
			t.Errorf("case %s failed, merged config should contain most specific config name, wanted %v got %v", tt.name, tt.expectedConfig, mergedConfig.Name)
		}
		mergedPolicy := mergedConfig.Spec.(*networking.DestinationRule).TrafficPolicy
		if !reflect.DeepEqual(mergedPolicy, tt.expectedPolicy) {
			t.Fatalf("case %s failed, want %+v, got %+v", tt.name, tt.expectedPolicy, mergedPolicy)
		}
	}
}

func TestSetDestinationRuleMerging(t *testing.T) {
	ps := NewPushContext()
	ps.exportToDefaults.destinationRule = map[visibility.Instance]bool{visibility.Public: true}
	testhost := "httpbin.org"
	destinationRuleNamespace1 := config.Config{
		Meta: config.Meta{
			Name:      "rule1",
			Namespace: "test",
		},
		Spec: &networking.DestinationRule{
			Host: testhost,
			Subsets: []*networking.Subset{
				{
					Name: "subset1",
				},
				{
					Name: "subset2",
				},
			},
		},
	}
	destinationRuleNamespace2 := config.Config{
		Meta: config.Meta{
			Name:      "rule2",
			Namespace: "test",
		},
		Spec: &networking.DestinationRule{
			Host: testhost,
			Subsets: []*networking.Subset{
				{
					Name: "subset3",
				},
				{
					Name: "subset4",
				},
			},
		},
	}
	ps.SetDestinationRules([]config.Config{destinationRuleNamespace1, destinationRuleNamespace2})
	subsetsLocal := ps.destinationRuleIndex.namespaceLocal["test"].destRule[host.Name(testhost)].Spec.(*networking.DestinationRule).Subsets
	subsetsExport := ps.destinationRuleIndex.exportedByNamespace["test"].destRule[host.Name(testhost)].Spec.(*networking.DestinationRule).Subsets
	if len(subsetsLocal) != 4 {
		t.Errorf("want %d, but got %d", 4, len(subsetsLocal))
	}

	if len(subsetsExport) != 4 {
		t.Errorf("want %d, but got %d", 4, len(subsetsExport))
	}
}

func TestSetDestinationRuleWithExportTo(t *testing.T) {
	ps := NewPushContext()
	ps.Mesh = &meshconfig.MeshConfig{RootNamespace: "istio-system"}
	testhost := "httpbin.org"
	appHost := "foo.app.org"
	wildcardHost1 := "*.org"
	wildcardHost2 := "*.app.org"
	destinationRuleNamespace1 := config.Config{
		Meta: config.Meta{
			Name:      "rule1",
			Namespace: "test1",
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{".", "ns1"},
			Subsets: []*networking.Subset{
				{
					Name: "subset1",
				},
				{
					Name: "subset2",
				},
			},
		},
	}
	destinationRuleNamespace2 := config.Config{
		Meta: config.Meta{
			Name:      "rule2",
			Namespace: "test2",
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{"test2", "ns1", "test1"},
			Subsets: []*networking.Subset{
				{
					Name: "subset3",
				},
				{
					Name: "subset4",
				},
			},
		},
	}
	destinationRuleNamespace3 := config.Config{
		Meta: config.Meta{
			Name:      "rule3",
			Namespace: "test3",
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{"test1", "test2", "*"},
			Subsets: []*networking.Subset{
				{
					Name: "subset5",
				},
				{
					Name: "subset6",
				},
			},
		},
	}
	destinationRuleRootNamespace := config.Config{
		Meta: config.Meta{
			Name:      "rule4",
			Namespace: "istio-system",
		},
		Spec: &networking.DestinationRule{
			Host: testhost,
			Subsets: []*networking.Subset{
				{
					Name: "subset7",
				},
				{
					Name: "subset8",
				},
			},
		},
	}
	destinationRuleRootNamespaceLocal := config.Config{
		Meta: config.Meta{
			Name:      "rule1",
			Namespace: "istio-system",
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{"."},
			Subsets: []*networking.Subset{
				{
					Name: "subset9",
				},
				{
					Name: "subset10",
				},
			},
		},
	}
	destinationRuleRootNamespaceLocalWithWildcardHost1 := config.Config{
		Meta: config.Meta{
			Name:      "rule2",
			Namespace: "istio-system",
		},
		Spec: &networking.DestinationRule{
			Host:     wildcardHost1,
			ExportTo: []string{"."},
			Subsets: []*networking.Subset{
				{
					Name: "subset11",
				},
				{
					Name: "subset12",
				},
			},
		},
	}
	destinationRuleRootNamespaceLocalWithWildcardHost2 := config.Config{
		Meta: config.Meta{
			Name:      "rule3",
			Namespace: "istio-system",
		},
		Spec: &networking.DestinationRule{
			Host:     wildcardHost2,
			ExportTo: []string{"."},
			Subsets: []*networking.Subset{
				{
					Name: "subset13",
				},
				{
					Name: "subset14",
				},
			},
		},
	}
	ps.SetDestinationRules([]config.Config{
		destinationRuleNamespace1, destinationRuleNamespace2,
		destinationRuleNamespace3, destinationRuleRootNamespace, destinationRuleRootNamespaceLocal,
		destinationRuleRootNamespaceLocalWithWildcardHost1, destinationRuleRootNamespaceLocalWithWildcardHost2,
	})
	cases := []struct {
		proxyNs     string
		serviceNs   string
		host        string
		wantSubsets []string
	}{
		{
			proxyNs:     "test1",
			serviceNs:   "test1",
			host:        testhost,
			wantSubsets: []string{"subset1", "subset2"},
		},
		{
			proxyNs:     "test1",
			serviceNs:   "test2",
			host:        testhost,
			wantSubsets: []string{"subset1", "subset2"},
		},
		{
			proxyNs:     "test2",
			serviceNs:   "test1",
			host:        testhost,
			wantSubsets: []string{"subset3", "subset4"},
		},
		{
			proxyNs:     "test3",
			serviceNs:   "test1",
			host:        testhost,
			wantSubsets: []string{"subset5", "subset6"},
		},
		{
			proxyNs:     "ns1",
			serviceNs:   "test1",
			host:        testhost,
			wantSubsets: []string{"subset1", "subset2"},
		},
		{
			proxyNs:     "ns1",
			serviceNs:   "random",
			host:        testhost,
			wantSubsets: []string{"subset7", "subset8"},
		},
		{
			proxyNs:     "random",
			serviceNs:   "random",
			host:        testhost,
			wantSubsets: []string{"subset7", "subset8"},
		},
		{
			proxyNs:     "test3",
			serviceNs:   "random",
			host:        testhost,
			wantSubsets: []string{"subset5", "subset6"},
		},
		{
			proxyNs:     "istio-system",
			serviceNs:   "random",
			host:        testhost,
			wantSubsets: []string{"subset9", "subset10"},
		},
		{
			proxyNs:     "istio-system",
			serviceNs:   "istio-system",
			host:        testhost,
			wantSubsets: []string{"subset9", "subset10"},
		},
		{
			proxyNs:     "istio-system",
			serviceNs:   "istio-system",
			host:        appHost,
			wantSubsets: []string{"subset13", "subset14"},
		},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%s-%s", tt.proxyNs, tt.serviceNs), func(t *testing.T) {
			destRuleConfig := ps.destinationRule(tt.proxyNs,
				&Service{
					Hostname: host.Name(tt.host),
					Attributes: ServiceAttributes{
						Namespace: tt.serviceNs,
					},
				})
			if destRuleConfig == nil {
				t.Fatalf("proxy in %s namespace: dest rule is nil, expected subsets %+v", tt.proxyNs, tt.wantSubsets)
			}
			destRule := destRuleConfig.Spec.(*networking.DestinationRule)
			var gotSubsets []string
			for _, ss := range destRule.Subsets {
				gotSubsets = append(gotSubsets, ss.Name)
			}
			if !reflect.DeepEqual(gotSubsets, tt.wantSubsets) {
				t.Fatalf("want %+v, got %+v", tt.wantSubsets, gotSubsets)
			}
		})
	}
}

func TestVirtualServiceWithExportTo(t *testing.T) {
	ps := NewPushContext()
	env := &Environment{Watcher: mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "zzz"})}
	ps.Mesh = env.Mesh()
	configStore := NewFakeStore()
	gatewayName := "default/gateway"

	rule1 := config.Config{
		Meta: config.Meta{
			Name:             "rule1",
			Namespace:        "test1",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"rule1.com"},
			ExportTo: []string{".", "ns1"},
		},
	}
	rule2 := config.Config{
		Meta: config.Meta{
			Name:             "rule2",
			Namespace:        "test2",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"rule2.com"},
			ExportTo: []string{"test2", "ns1", "test1"},
		},
	}
	rule2Gw := config.Config{
		Meta: config.Meta{
			Name:             "rule2Gw",
			Namespace:        "test2",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &networking.VirtualService{
			Gateways: []string{gatewayName, constants.IstioMeshGateway},
			Hosts:    []string{"rule2gw.com"},
			ExportTo: []string{"test2", "ns1", "test1"},
		},
	}
	rule3 := config.Config{
		Meta: config.Meta{
			Name:             "rule3",
			Namespace:        "test3",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &networking.VirtualService{
			Gateways: []string{constants.IstioMeshGateway},
			Hosts:    []string{"rule3.com"},
			ExportTo: []string{"test1", "test2", "*"},
		},
	}
	rule3Gw := config.Config{
		Meta: config.Meta{
			Name:             "rule3Gw",
			Namespace:        "test3",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &networking.VirtualService{
			Gateways: []string{gatewayName},
			Hosts:    []string{"rule3gw.com"},
			ExportTo: []string{"test1", "test2", "*"},
		},
	}
	rootNS := config.Config{
		Meta: config.Meta{
			Name:             "zzz",
			Namespace:        "zzz",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &networking.VirtualService{
			Hosts: []string{"rootNS.com"},
		},
	}

	for _, c := range []config.Config{rule1, rule2, rule3, rule2Gw, rule3Gw, rootNS} {
		if _, err := configStore.Create(c); err != nil {
			t.Fatalf("could not create %v", c.Name)
		}
	}

	store := istioConfigStore{ConfigStore: configStore}
	env.IstioConfigStore = &store
	ps.initDefaultExportMaps()
	if err := ps.initVirtualServices(env); err != nil {
		t.Fatalf("init virtual services failed: %v", err)
	}

	cases := []struct {
		proxyNs   string
		gateway   string
		wantHosts []string
	}{
		{
			proxyNs:   "test1",
			wantHosts: []string{"rule1.com", "rule2.com", "rule2gw.com", "rule3.com", "rootNS.com"},
			gateway:   constants.IstioMeshGateway,
		},
		{
			proxyNs:   "test2",
			wantHosts: []string{"rule2.com", "rule2gw.com", "rule3.com", "rootNS.com"},
			gateway:   constants.IstioMeshGateway,
		},
		{
			proxyNs:   "ns1",
			wantHosts: []string{"rule1.com", "rule2.com", "rule2gw.com", "rule3.com", "rootNS.com"},
			gateway:   constants.IstioMeshGateway,
		},
		{
			proxyNs:   "random",
			wantHosts: []string{"rule3.com", "rootNS.com"},
			gateway:   constants.IstioMeshGateway,
		},
		{
			proxyNs:   "test1",
			wantHosts: []string{"rule2gw.com", "rule3gw.com"},
			gateway:   gatewayName,
		},
		{
			proxyNs:   "test2",
			wantHosts: []string{"rule2gw.com", "rule3gw.com"},
			gateway:   gatewayName,
		},
		{
			proxyNs:   "ns1",
			wantHosts: []string{"rule2gw.com", "rule3gw.com"},
			gateway:   gatewayName,
		},
		{
			proxyNs:   "random",
			wantHosts: []string{"rule3gw.com"},
			gateway:   gatewayName,
		},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%s-%s", tt.proxyNs, tt.gateway), func(t *testing.T) {
			rules := ps.VirtualServicesForGateway(tt.proxyNs, tt.gateway)
			gotHosts := make([]string, 0)
			for _, r := range rules {
				vs := r.Spec.(*networking.VirtualService)
				gotHosts = append(gotHosts, vs.Hosts...)
			}
			if !reflect.DeepEqual(gotHosts, tt.wantHosts) {
				t.Errorf("want %+v, got %+v", tt.wantHosts, gotHosts)
			}
		})
	}
}

func TestInitVirtualService(t *testing.T) {
	ps := NewPushContext()
	env := &Environment{Watcher: mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})}
	ps.Mesh = env.Mesh()
	configStore := NewFakeStore()
	gatewayName := "ns1/gateway"

	vs1 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "vs1",
			Namespace:        "ns1",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"gateway"},
			Http: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
							},
						},
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Exact{Exact: "/login"},
							},
						},
					},
					Delegate: &networking.Delegate{
						Name:      "vs2",
						Namespace: "ns2",
					},
				},
			},
		},
	}
	vs2 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "vs2",
			Namespace:        "ns2",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{},
			Gateways: []string{gatewayName},
			Http: []*networking.HTTPRoute{
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "test",
								Port: &networking.PortSelector{
									Number: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	for _, c := range []config.Config{vs1, vs2} {
		if _, err := configStore.Create(c); err != nil {
			t.Fatalf("could not create %v", c.Name)
		}
	}

	store := istioConfigStore{ConfigStore: configStore}
	env.IstioConfigStore = &store
	ps.initDefaultExportMaps()
	if err := ps.initVirtualServices(env); err != nil {
		t.Fatalf("init virtual services failed: %v", err)
	}

	t.Run("resolve shortname", func(t *testing.T) {
		rules := ps.VirtualServicesForGateway("ns1", gatewayName)
		if len(rules) != 1 {
			t.Fatalf("wanted 1 virtualservice for gateway %s, actually got %d", gatewayName, len(rules))
		}
		gotHTTPHosts := make([]string, 0)
		for _, r := range rules {
			vs := r.Spec.(*networking.VirtualService)
			for _, route := range vs.GetHttp() {
				for _, dst := range route.Route {
					gotHTTPHosts = append(gotHTTPHosts, dst.Destination.Host)
				}
			}
		}
		if !reflect.DeepEqual(gotHTTPHosts, []string{"test.ns2"}) {
			t.Errorf("got %+v", gotHTTPHosts)
		}
	})
}

func TestServiceWithExportTo(t *testing.T) {
	ps := NewPushContext()
	env := &Environment{Watcher: mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "zzz"})}
	ps.Mesh = env.Mesh()

	svc1 := &Service{
		Hostname: "svc1",
		Attributes: ServiceAttributes{
			Namespace: "test1",
			ExportTo:  map[visibility.Instance]bool{visibility.Private: true, visibility.Instance("ns1"): true},
		},
	}
	svc2 := &Service{
		Hostname: "svc2",
		Attributes: ServiceAttributes{
			Namespace: "test2",
			ExportTo: map[visibility.Instance]bool{
				visibility.Instance("test1"): true,
				visibility.Instance("ns1"):   true,
				visibility.Instance("test2"): true,
			},
		},
	}
	svc3 := &Service{
		Hostname: "svc3",
		Attributes: ServiceAttributes{
			Namespace: "test3",
			ExportTo: map[visibility.Instance]bool{
				visibility.Instance("test1"): true,
				visibility.Public:            true,
				visibility.Instance("test2"): true,
			},
		},
	}
	svc4 := &Service{
		Hostname: "svc4",
		Attributes: ServiceAttributes{
			Namespace: "test4",
		},
	}
	env.ServiceDiscovery = &localServiceDiscovery{
		services: []*Service{svc1, svc2, svc3, svc4},
	}
	ps.initDefaultExportMaps()
	if err := ps.initServiceRegistry(env); err != nil {
		t.Fatalf("init services failed: %v", err)
	}

	cases := []struct {
		proxyNs   string
		wantHosts []string
	}{
		{
			proxyNs:   "test1",
			wantHosts: []string{"svc1", "svc2", "svc3", "svc4"},
		},
		{
			proxyNs:   "test2",
			wantHosts: []string{"svc2", "svc3", "svc4"},
		},
		{
			proxyNs:   "ns1",
			wantHosts: []string{"svc1", "svc2", "svc3", "svc4"},
		},
		{
			proxyNs:   "random",
			wantHosts: []string{"svc3", "svc4"},
		},
	}
	for _, tt := range cases {
		services := ps.servicesExportedToNamespace(tt.proxyNs)
		gotHosts := make([]string, 0)
		for _, r := range services {
			gotHosts = append(gotHosts, string(r.Hostname))
		}
		if !reflect.DeepEqual(gotHosts, tt.wantHosts) {
			t.Errorf("proxy in %s namespace: want %+v, got %+v", tt.proxyNs, tt.wantHosts, gotHosts)
		}
	}
}

var _ ServiceDiscovery = &localServiceDiscovery{}

// MockDiscovery is an in-memory ServiceDiscover with mock services
type localServiceDiscovery struct {
	services         []*Service
	serviceInstances []*ServiceInstance

	NetworkGatewaysHandler
}

var _ ServiceDiscovery = &localServiceDiscovery{}

func (l *localServiceDiscovery) Services() []*Service {
	return l.services
}

func (l *localServiceDiscovery) GetService(host.Name) *Service {
	panic("implement me")
}

func (l *localServiceDiscovery) InstancesByPort(*Service, int, labels.Collection) []*ServiceInstance {
	return l.serviceInstances
}

func (l *localServiceDiscovery) GetProxyServiceInstances(*Proxy) []*ServiceInstance {
	panic("implement me")
}

func (l *localServiceDiscovery) GetProxyWorkloadLabels(*Proxy) labels.Collection {
	panic("implement me")
}

func (l *localServiceDiscovery) GetIstioServiceAccounts(*Service, []int) []string {
	return nil
}

func (l *localServiceDiscovery) NetworkGateways() []NetworkGateway {
	// TODO implement fromRegistry logic from kube controller if needed
	return nil
}

func (l *localServiceDiscovery) MCSServices() []MCSServiceInfo {
	return nil
}
