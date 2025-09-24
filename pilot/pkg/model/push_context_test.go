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
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/types"

	extensions "istio.io/api/extensions/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	securityBeta "istio.io/api/security/v1beta1"
	selectorpb "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
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
			&PushRequest{Full: true, Forced: true},
			PushRequest{Full: true, Forced: true},
		},
		{
			"right nil",
			&PushRequest{Full: true, Forced: true},
			nil,
			PushRequest{Full: true, Forced: true},
		},
		{
			"simple merge",
			&PushRequest{
				Full:  true,
				Push:  push0,
				Start: t0,
				ConfigsUpdated: sets.Set[ConfigKey]{
					{Kind: kind.Kind(1), Namespace: "ns1"}: {},
				},
				Reason: NewReasonStats(ServiceUpdate, ServiceUpdate),
				Forced: true,
			},
			&PushRequest{
				Full:  false,
				Push:  push1,
				Start: t1,
				ConfigsUpdated: sets.Set[ConfigKey]{
					{Kind: kind.Kind(2), Namespace: "ns2"}: {},
				},
				Reason: NewReasonStats(EndpointUpdate),
				Forced: false,
			},
			PushRequest{
				Full:  true,
				Push:  push1,
				Start: t0,
				ConfigsUpdated: sets.Set[ConfigKey]{
					{Kind: kind.Kind(1), Namespace: "ns1"}: {},
					{Kind: kind.Kind(2), Namespace: "ns2"}: {},
				},
				Reason: NewReasonStats(ServiceUpdate, ServiceUpdate, EndpointUpdate),
				Forced: true,
			},
		},
		{
			"skip config type merge: one empty",
			&PushRequest{Full: true, ConfigsUpdated: nil, Forced: true},
			&PushRequest{Full: true, ConfigsUpdated: sets.Set[ConfigKey]{{
				Kind: kind.Kind(2),
			}: {}}},
			PushRequest{Full: true, ConfigsUpdated: sets.Set[ConfigKey]{{
				Kind: kind.Kind(2),
			}: {}}, Reason: nil, Forced: true},
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
	reqA := &PushRequest{Reason: make(ReasonStats)}
	reqB := &PushRequest{Reason: NewReasonStats(ServiceUpdate, ProxyUpdate)}
	for i := 0; i < 50; i++ {
		go func() {
			reqA.CopyMerge(reqB)
		}()
	}
	if reqA.Reason.Count() != 0 {
		t.Fatalf("reqA modified: %v", reqA.Reason)
	}
	if reqB.Reason.Count() != 2 {
		t.Fatalf("reqB modified: %v", reqB.Reason)
	}
}

func TestEnvoyFilters(t *testing.T) {
	envoyFilters := []*EnvoyFilterWrapper{
		convertToEnvoyFilterWrapper(&config.Config{
			Meta: config.Meta{
				Name:      "ef1",
				Namespace: "test-ns",
			},
			Spec: &networking.EnvoyFilter{
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: map[string]string{"app": "v1"},
				},
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						ApplyTo: networking.EnvoyFilter_LISTENER,
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{
								ProxyVersion: "1\\.4.*",
							},
						},
						// we don't care about the patch content in this test, but it must be non-nil
						Patch: &networking.EnvoyFilter_Patch{},
					},
				},
			},
		}),
		convertToEnvoyFilterWrapper(&config.Config{
			Meta: config.Meta{
				Name:      "ef2",
				Namespace: "test-ns",
			},
			Spec: &networking.EnvoyFilter{
				WorkloadSelector: &networking.WorkloadSelector{
					Labels: map[string]string{"app": "v1"},
				},
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						ApplyTo: networking.EnvoyFilter_CLUSTER,
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{
								ProxyVersion: "1\\.4.*",
							},
						},
						// we don't care about the patch content in this test, but it must be non-nil
						Patch: &networking.EnvoyFilter_Patch{},
					},
				},
			},
		}),
		convertToEnvoyFilterWrapper(&config.Config{
			Meta: config.Meta{
				Name:      "ef-with-target-ref",
				Namespace: "test-ns",
			},
			Spec: &networking.EnvoyFilter{
				TargetRefs: []*selectorpb.PolicyTargetReference{
					{
						Group: "gateway.networking.k8s.io",
						Kind:  "Gateway",
						Name:  "gateway-1",
					},
				},
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						ApplyTo: networking.EnvoyFilter_CLUSTER,
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{
								ProxyVersion: "1\\.4.*",
							},
						},
						// we don't care about the patch content in this test, but it must be non-nil
						Patch: &networking.EnvoyFilter_Patch{},
					},
				},
			},
		}),
		convertToEnvoyFilterWrapper(&config.Config{
			Meta: config.Meta{
				Name:      "ef-for-waypoint",
				Namespace: "test-ns",
			},
			Spec: &networking.EnvoyFilter{
				TargetRefs: []*selectorpb.PolicyTargetReference{
					{
						Group: "gateway.networking.k8s.io",
						Kind:  "Gateway",
						Name:  "waypoint",
					},
				},
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						ApplyTo: networking.EnvoyFilter_CLUSTER,
						// we don't care about the patch content in this test, but it must be non-nil
						Patch: &networking.EnvoyFilter_Patch{},
					},
				},
			},
		}),
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
				Labels:          map[string]string{"app": "v1"},
				Metadata:        &NodeMetadata{IstioVersion: "1.4.0", Labels: map[string]string{"app": "v1"}},
				ConfigNamespace: "test-ns",
			},
			expectedListenerPatches: 2,
			expectedClusterPatches:  2,
		},
		{
			name: "proxy in root namespace matches an envoyfilter",
			proxy: &Proxy{
				Labels:          map[string]string{"app": "v1"},
				Metadata:        &NodeMetadata{IstioVersion: "1.4.0", Labels: map[string]string{"app": "v1"}},
				ConfigNamespace: "istio-system",
			},
			expectedListenerPatches: 1,
			expectedClusterPatches:  1,
		},
		{
			name: "proxy matches no envoyfilter",
			proxy: &Proxy{
				Labels:          map[string]string{"app": "v2"},
				Metadata:        &NodeMetadata{IstioVersion: "1.4.0", Labels: map[string]string{"app": "v2"}},
				ConfigNamespace: "test-ns",
			},
			expectedListenerPatches: 0,
			expectedClusterPatches:  0,
		},
		{
			name: "proxy matches envoyfilter in root ns",
			proxy: &Proxy{
				Labels:          map[string]string{"app": "v1"},
				Metadata:        &NodeMetadata{IstioVersion: "1.4.0", Labels: map[string]string{"app": "v1"}},
				ConfigNamespace: "test-n2",
			},
			expectedListenerPatches: 1,
			expectedClusterPatches:  1,
		},
		{
			name: "proxy version matches no envoyfilters",
			proxy: &Proxy{
				Labels:          map[string]string{"app": "v1"},
				Metadata:        &NodeMetadata{IstioVersion: "1.3.0", Labels: map[string]string{"app": "v1"}},
				ConfigNamespace: "test-ns",
			},
			expectedListenerPatches: 0,
			expectedClusterPatches:  0,
		},
		{
			name: "proxy matched target ref",
			proxy: &Proxy{
				Labels:          map[string]string{"gateway.networking.k8s.io/gateway-name": "gateway-1"},
				Metadata:        &NodeMetadata{IstioVersion: "1.4.0", Labels: map[string]string{"app": "v1"}},
				ConfigNamespace: "test-ns",
			},
			expectedListenerPatches: 0,
			expectedClusterPatches:  2,
		},
		{
			name: "waypoint matched",
			proxy: &Proxy{
				Type:            model.Waypoint,
				Labels:          map[string]string{"gateway.networking.k8s.io/gateway-name": "waypoint"},
				Metadata:        &NodeMetadata{IstioVersion: "1.4.0", Labels: map[string]string{"app": "v1"}},
				ConfigNamespace: "test-ns",
			},
			expectedListenerPatches: 0,
			expectedClusterPatches:  2,
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
	store := NewFakeStore()

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
	env.ConfigStore = store
	m := mesh.DefaultMeshConfig()
	env.Watcher = meshwatcher.NewTestWatcher(m)
	env.Init()

	// Init a new push context
	pc := NewPushContext()
	pc.initEnvoyFilters(env, nil, nil)
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

func buildPatchStruct(config string) *structpb.Struct {
	val := &structpb.Struct{}
	_ = protomarshal.UnmarshalString(config, val)
	return val
}

func TestEnvoyFilterOrderAcrossNamespaces(t *testing.T) {
	env := &Environment{}
	store := NewFakeStore()

	proxy := &Proxy{
		Metadata:        &NodeMetadata{IstioVersion: "foobar"},
		Labels:          map[string]string{"app": "v1"},
		ConfigNamespace: "test-ns",
	}

	envoyFilters := []config.Config{
		{
			Meta: config.Meta{Name: "filter-1", Namespace: "test-ns", GroupVersionKind: gvk.EnvoyFilter},
			Spec: &networking.EnvoyFilter{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
						Patch: &networking.EnvoyFilter_Patch{
							Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
							Value:     buildPatchStruct(`{"name": "filter-1"}`),
						},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
				Priority: -5,
			},
		},
		{
			Meta: config.Meta{Name: "filter-2", Namespace: "istio-system", GroupVersionKind: gvk.EnvoyFilter},
			Spec: &networking.EnvoyFilter{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
						Patch: &networking.EnvoyFilter_Patch{
							Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
							Value:     buildPatchStruct(`{"name": "filter-2"}`),
						},
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobar`},
						},
					},
				},
				Priority: -1,
			},
		},
	}

	expectedFilterOrder := []string{"test-ns/filter-1", "istio-system/filter-2"}
	for _, cfg := range envoyFilters {
		_, _ = store.Create(cfg)
	}
	env.ConfigStore = store
	m := mesh.DefaultMeshConfig()
	m.RootNamespace = "istio-system"
	env.Watcher = meshwatcher.NewTestWatcher(m)
	env.Init()

	// Init a new push context
	pc := NewPushContext()
	pc.Mesh = m
	pc.initEnvoyFilters(env, nil, nil)
	got := make([]string, 0)
	efs := pc.EnvoyFilters(proxy)
	for _, filter := range efs.Patches[networking.EnvoyFilter_HTTP_FILTER] {
		got = append(got, filter.Namespace+"/"+filter.Name)
	}
	if !slices.Equal(expectedFilterOrder, got) {
		t.Errorf("Envoy filters are not ordered as expected. expected: %v got: %v", expectedFilterOrder, got)
	}
}

func TestEnvoyFilterUpdate(t *testing.T) {
	ctime := time.Now()

	initialEnvoyFilters := []config.Config{
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

	cases := []struct {
		name    string
		creates []config.Config
		updates []config.Config
		deletes []ConfigKey
	}{
		{
			name: "create one",
			creates: []config.Config{
				{
					Meta: config.Meta{Name: "default-priority-2", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter},
					Spec: &networking.EnvoyFilter{
						ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
							{
								Patch: &networking.EnvoyFilter_Patch{},
								Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
									Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobaz`},
								},
							},
						},
					},
				},
			},
			updates: []config.Config{},
			deletes: []ConfigKey{},
		},
		{
			name:    "update one",
			creates: []config.Config{},
			updates: []config.Config{
				{
					Meta: config.Meta{Name: "default-priority", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter},
					Spec: &networking.EnvoyFilter{
						ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
							{
								Patch: &networking.EnvoyFilter_Patch{},
								Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
									Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobaz`},
								},
							},
						},
					},
				},
			},
			deletes: []ConfigKey{},
		},
		{
			name:    "delete one",
			creates: []config.Config{},
			updates: []config.Config{},
			deletes: []ConfigKey{{Kind: kind.EnvoyFilter, Name: "default-priority", Namespace: "testns"}},
		},
		{
			name: "create and delete one same namespace",
			creates: []config.Config{
				{
					Meta: config.Meta{Name: "default-priority-2", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter},
					Spec: &networking.EnvoyFilter{
						ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
							{
								Patch: &networking.EnvoyFilter_Patch{},
								Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
									Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobaz`},
								},
							},
						},
					},
				},
			},
			updates: []config.Config{},
			deletes: []ConfigKey{{Kind: kind.EnvoyFilter, Name: "default-priority", Namespace: "testns"}},
		},
		{
			name: "create and delete one different namespace",
			creates: []config.Config{
				{
					Meta: config.Meta{Name: "default-priority-2", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter},
					Spec: &networking.EnvoyFilter{
						ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
							{
								Patch: &networking.EnvoyFilter_Patch{},
								Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
									Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobaz`},
								},
							},
						},
					},
				},
			},
			updates: []config.Config{},
			deletes: []ConfigKey{{Kind: kind.EnvoyFilter, Name: "default-priority", Namespace: "testns-1"}},
		},
		{
			name: "create, update delete",
			creates: []config.Config{
				{
					Meta: config.Meta{Name: "default-priority-2", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter},
					Spec: &networking.EnvoyFilter{
						ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
							{
								Patch: &networking.EnvoyFilter_Patch{},
								Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
									Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobaz`},
								},
							},
						},
					},
				},
				{
					Meta: config.Meta{Name: "default-priority-3", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter},
					Spec: &networking.EnvoyFilter{
						ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
							{
								Patch: &networking.EnvoyFilter_Patch{},
								Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
									Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobaz`},
								},
							},
						},
					},
				},
			},
			updates: []config.Config{
				{
					Meta: config.Meta{Name: "default-priority", Namespace: "testns", GroupVersionKind: gvk.EnvoyFilter},
					Spec: &networking.EnvoyFilter{
						ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
							{
								Patch: &networking.EnvoyFilter_Patch{},
								Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
									Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobaz`},
								},
							},
						},
					},
				},
				{
					Meta: config.Meta{Name: "default-priority", Namespace: "testns-1", GroupVersionKind: gvk.EnvoyFilter},
					Spec: &networking.EnvoyFilter{
						ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
							{
								Patch: &networking.EnvoyFilter_Patch{},
								Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
									Proxy: &networking.EnvoyFilter_ProxyMatch{ProxyVersion: `foobaz`},
								},
							},
						},
					},
				},
			},
			deletes: []ConfigKey{{Kind: kind.EnvoyFilter, Name: "b-medium-priority", Namespace: "testns-1"}},
		},
		{
			name:    "delete entire namespace",
			creates: []config.Config{},
			updates: []config.Config{},
			deletes: []ConfigKey{
				{Kind: kind.EnvoyFilter, Name: "default-priority", Namespace: "testns-1"},
				{Kind: kind.EnvoyFilter, Name: "b-medium-priority", Namespace: "testns-1"},
				{Kind: kind.EnvoyFilter, Name: "a-medium-priority", Namespace: "testns-1"},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			env := &Environment{}
			store := NewFakeStore()
			for _, cfg := range initialEnvoyFilters {
				_, _ = store.Create(cfg)
			}
			env.ConfigStore = store
			m := mesh.DefaultMeshConfig()
			env.Watcher = meshwatcher.NewTestWatcher(m)
			env.Init()

			// Init a new push context
			pc1 := NewPushContext()
			pc1.initEnvoyFilters(env, nil, nil)

			// Update store with incoming changes
			creates := map[ConfigKey]config.Config{}
			for _, cfg := range tt.creates {
				if _, err := store.Create(cfg); err != nil {
					t.Errorf("Error creating config %s/%s", cfg.Namespace, cfg.Name)
				}
				creates[ConfigKey{Name: cfg.Name, Namespace: cfg.Namespace, Kind: kind.EnvoyFilter}] = cfg
			}
			updates := map[ConfigKey]config.Config{}
			for _, cfg := range tt.updates {
				if _, err := store.Update(cfg); err != nil {
					t.Errorf("Error updating config %s/%s", cfg.Namespace, cfg.Name)
				}
				updates[ConfigKey{Name: cfg.Name, Namespace: cfg.Namespace, Kind: kind.EnvoyFilter}] = cfg
			}
			deletes := sets.Set[ConfigKey]{}
			for _, key := range tt.deletes {
				store.Delete(gvk.EnvoyFilter, key.Name, key.Namespace, nil)
				deletes.Insert(key)
			}

			createSet := sets.New(maps.Keys(creates)...)
			updateSet := sets.New(maps.Keys(updates)...)
			changes := deletes.Union(createSet).Union(updateSet)

			pc2 := NewPushContext()
			pc2.initEnvoyFilters(env, changes, pc1.envoyFiltersByNamespace)

			total2 := 0
			for ns, envoyFilters := range pc2.envoyFiltersByNamespace {
				total2 += len(envoyFilters)
				for _, ef := range envoyFilters {
					key := ConfigKey{Kind: kind.EnvoyFilter, Namespace: ns, Name: ef.Name}
					previousVersion := slices.FindFunc(pc1.envoyFiltersByNamespace[ns], func(e *EnvoyFilterWrapper) bool {
						return e.Name == ef.Name
					})
					switch {
					// Newly created Envoy filter.
					case createSet.Contains(key):
						cfg := creates[key]
						// If the filter is newly created, it should not have a previous version.
						if previousVersion != nil {
							t.Errorf("Created Envoy filter %s/%s already existed", ns, ef.Name)
						}
						// Validate that the generated filter is the same as the one created.
						if !reflect.DeepEqual(ef, convertToEnvoyFilterWrapper(&cfg)) {
							t.Errorf("Unexpected envoy filter generated %s/%s", ns, ef.Name)
						}
					// Updated Envoy filter.
					case updateSet.Contains(key):
						cfg := updates[key]
						// If the filter is updated, it should have a previous version.
						if previousVersion == nil {
							t.Errorf("Updated Envoy filter %s/%s did not exist", ns, ef.Name)
						} else if reflect.DeepEqual(*previousVersion, ef) {
							// Validate that the generated filter is different from the previous version.
							t.Errorf("Envoy filter %s/%s was not updated", ns, ef.Name)
						}
						// Validate that the generated filter is the same as the one updated.
						if !reflect.DeepEqual(ef, convertToEnvoyFilterWrapper(&cfg)) {
							t.Errorf("Unexpected envoy filter generated %s/%s", ns, ef.Name)
						}
					// Deleted Envoy filter.
					case deletes.Contains(key):
						t.Errorf("Found deleted EnvoyFilter %s/%s", ns, ef.Name)
					// Unchanged Envoy filter.
					default:
						if previousVersion == nil {
							t.Errorf("Unchanged EnvoyFilter was not previously found %s/%s", ns, ef.Name)
						} else {
							if *previousVersion != ef {
								// Validate that Unchanged filter is not regenerated when config optimization is enabled.
								t.Errorf("Unchanged EnvoyFilter is different from original %s/%s", ns, ef.Name)
							}
							if !reflect.DeepEqual(*previousVersion, ef) {
								t.Errorf("Envoy filter %s/%s has unexpected change", ns, ef.Name)
							}
						}
					}
				}
			}

			total1 := 0
			// Validate that empty namespace is deleted when all filters in that namespace are deleted.
			for ns, envoyFilters := range pc1.envoyFiltersByNamespace {
				total1 += len(envoyFilters)
				deleted := 0
				for _, ef := range envoyFilters {
					key := ConfigKey{Kind: kind.EnvoyFilter, Namespace: ns, Name: ef.Name}
					if deletes.Contains(key) {
						deleted++
					}
				}

				if deleted == len(envoyFilters) {
					if _, ok := pc2.envoyFiltersByNamespace[ns]; ok {
						t.Errorf("Empty Namespace %s was not deleted", ns)
					}
				}
			}

			if total2 != total1+len(tt.creates)-len(tt.deletes) {
				t.Errorf("Expected %d envoy filters, found %d", total1+len(tt.creates)-len(tt.deletes), total2)
			}
		})
	}
}

func TestWasmPlugins(t *testing.T) {
	env := &Environment{}
	store := NewFakeStore()

	wasmPlugins := map[string]config.Config{
		"invalid-type": {
			Meta: config.Meta{Name: "invalid-type", Namespace: constants.IstioSystemNamespace, GroupVersionKind: gvk.WasmPlugin},
			Spec: &networking.DestinationRule{},
		},
		"invalid-url": {
			Meta: config.Meta{Name: "invalid-url", Namespace: constants.IstioSystemNamespace, GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHN,
				Priority: &wrapperspb.Int32Value{Value: 5},
				Url:      "notavalid%%Url;",
			},
		},
		"authn-low-prio-all": {
			Meta: config.Meta{Name: "authn-low-prio-all", Namespace: "testns-1", GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHN,
				Priority: &wrapperspb.Int32Value{Value: 10},
				Url:      "file:///etc/istio/filters/authn.wasm",
				PluginConfig: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"test": {
							Kind: &structpb.Value_StringValue{StringValue: "test"},
						},
					},
				},
				Sha256: "f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2",
			},
		},
		"authn-low-prio-all-network": {
			Meta: config.Meta{Name: "authn-low-prio-all-network", Namespace: "testns-1", GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Type:     extensions.PluginType_NETWORK,
				Phase:    extensions.PluginPhase_AUTHN,
				Priority: &wrapperspb.Int32Value{Value: 10},
				Url:      "file:///etc/istio/filters/authn.wasm",
				PluginConfig: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"test": {
							Kind: &structpb.Value_StringValue{StringValue: "test"},
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
				Priority: &wrapperspb.Int32Value{Value: 5},
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
				Priority: &wrapperspb.Int32Value{Value: 50},
			},
		},
		"global-authn-high-prio-app": {
			Meta: config.Meta{Name: "global-authn-high-prio-app", Namespace: constants.IstioSystemNamespace, GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHN,
				Priority: &wrapperspb.Int32Value{Value: 1000},
				Selector: &selectorpb.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "productpage",
					},
				},
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  selectorpb.WorkloadMode_SERVER,
						Ports: []*selectorpb.PortSelector{{Number: 1234}},
					},
				},
			},
		},
		"global-authz-med-prio-app": {
			Meta: config.Meta{Name: "global-authz-med-prio-app", Namespace: constants.IstioSystemNamespace, GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHZ,
				Priority: &wrapperspb.Int32Value{Value: 50},
				Selector: &selectorpb.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "productpage",
					},
				},
				Match: []*extensions.WasmPlugin_TrafficSelector{
					{
						Mode:  selectorpb.WorkloadMode_SERVER,
						Ports: []*selectorpb.PortSelector{{Number: 1235}},
					},
				},
			},
		},
		"authz-high-prio-ingress": {
			Meta: config.Meta{Name: "authz-high-prio-ingress", Namespace: "testns-2", GroupVersionKind: gvk.WasmPlugin},
			Spec: &extensions.WasmPlugin{
				Phase:    extensions.PluginPhase_AUTHZ,
				Priority: &wrapperspb.Int32Value{Value: 1000},
			},
		},
	}

	testCases := []struct {
		name               string
		node               *Proxy
		listenerInfo       WasmPluginListenerInfo
		pluginType         WasmPluginType
		expectedExtensions map[extensions.PluginPhase][]*WasmPluginWrapper
	}{
		{
			name:               "nil proxy",
			node:               nil,
			listenerInfo:       anyListener,
			pluginType:         WasmPluginTypeHTTP,
			expectedExtensions: nil,
		},
		{
			name: "nomatch",
			node: &Proxy{
				ConfigNamespace: "other",
				Metadata:        &NodeMetadata{},
			},
			listenerInfo:       anyListener,
			pluginType:         WasmPluginTypeHTTP,
			expectedExtensions: map[extensions.PluginPhase][]*WasmPluginWrapper{},
		},
		{
			name: "ingress",
			node: &Proxy{
				ConfigNamespace: "other",
				Labels: map[string]string{
					"istio": "ingressgateway",
				},
				Metadata: &NodeMetadata{
					Labels: map[string]string{
						"istio": "ingressgateway",
					},
				},
			},
			listenerInfo: anyListener,
			pluginType:   WasmPluginTypeHTTP,
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
				Labels: map[string]string{
					"istio": "ingressgateway",
				},
				Metadata: &NodeMetadata{
					Labels: map[string]string{
						"istio": "ingressgateway",
					},
				},
			},
			listenerInfo: anyListener,
			pluginType:   WasmPluginTypeHTTP,
			expectedExtensions: map[extensions.PluginPhase][]*WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					convertToWasmPluginWrapper(wasmPlugins["authn-med-prio-all"]),
					convertToWasmPluginWrapper(wasmPlugins["authn-low-prio-all"]),
					convertToWasmPluginWrapper(wasmPlugins["global-authn-low-prio-ingress"]),
				},
			},
		},
		{
			name: "ingress-testns-1-network",
			node: &Proxy{
				ConfigNamespace: "testns-1",
				Labels: map[string]string{
					"istio": "ingressgateway",
				},
				Metadata: &NodeMetadata{
					Labels: map[string]string{
						"istio": "ingressgateway",
					},
				},
			},
			listenerInfo: anyListener,
			pluginType:   WasmPluginTypeNetwork,
			expectedExtensions: map[extensions.PluginPhase][]*WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					convertToWasmPluginWrapper(wasmPlugins["authn-low-prio-all-network"]),
				},
			},
		},
		{
			name: "ingress-testns-1-any",
			node: &Proxy{
				ConfigNamespace: "testns-1",
				Labels: map[string]string{
					"istio": "ingressgateway",
				},
				Metadata: &NodeMetadata{
					Labels: map[string]string{
						"istio": "ingressgateway",
					},
				},
			},
			listenerInfo: anyListener,
			pluginType:   WasmPluginTypeAny,
			expectedExtensions: map[extensions.PluginPhase][]*WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					convertToWasmPluginWrapper(wasmPlugins["authn-med-prio-all"]),
					convertToWasmPluginWrapper(wasmPlugins["authn-low-prio-all"]),
					convertToWasmPluginWrapper(wasmPlugins["authn-low-prio-all-network"]),
					convertToWasmPluginWrapper(wasmPlugins["global-authn-low-prio-ingress"]),
				},
			},
		},
		{
			name: "testns-2",
			node: &Proxy{
				ConfigNamespace: "testns-2",
				Labels: map[string]string{
					"app": "productpage",
				},
				Metadata: &NodeMetadata{
					Labels: map[string]string{
						"app": "productpage",
					},
				},
			},
			listenerInfo: anyListener,
			pluginType:   WasmPluginTypeHTTP,
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
		{
			// Detailed tests regarding TrafficSelector are in extension_test.go
			// Just test the integrity here.
			// This testcase is identical with "testns-2", but `listenerInfo`` is specified.
			// 1. `global-authn-high-prio-app` matched, because it has a port matching clause with "1234"
			// 2. `authz-high-prio-ingress` matched, because it does not have any `match` clause
			// 3. `global-authz-med-prio-app` not matched, because it has a port matching clause with "1235"
			name: "testns-2-with-port-match",
			node: &Proxy{
				ConfigNamespace: "testns-2",
				Labels: map[string]string{
					"app": "productpage",
				},
				Metadata: &NodeMetadata{
					Labels: map[string]string{
						"app": "productpage",
					},
				},
			},
			listenerInfo: WasmPluginListenerInfo{
				Port:  1234,
				Class: istionetworking.ListenerClassSidecarInbound,
			},
			pluginType: WasmPluginTypeHTTP,
			expectedExtensions: map[extensions.PluginPhase][]*WasmPluginWrapper{
				extensions.PluginPhase_AUTHN: {
					convertToWasmPluginWrapper(wasmPlugins["global-authn-high-prio-app"]),
				},
				extensions.PluginPhase_AUTHZ: {
					convertToWasmPluginWrapper(wasmPlugins["authz-high-prio-ingress"]),
				},
			},
		},
	}

	for _, config := range wasmPlugins {
		store.Create(config)
	}
	env.ConfigStore = store
	m := mesh.DefaultMeshConfig()
	env.Watcher = meshwatcher.NewTestWatcher(m)
	env.Init()

	// Init a new push context
	pc := NewPushContext()
	pc.Mesh = m
	pc.initWasmPlugins(env)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := pc.WasmPluginsByListenerInfo(tc.node, tc.listenerInfo, tc.pluginType)
			if !reflect.DeepEqual(tc.expectedExtensions, result) {
				t.Errorf("WasmPlugins did not match expectations\n\ngot: %v\n\nexpected: %v", result, tc.expectedExtensions)
			}
		})
	}
}

func TestServiceIndex(t *testing.T) {
	g := NewWithT(t)
	env := NewEnvironment()
	env.ConfigStore = NewFakeStore()
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
					ExportTo:  sets.New(visibility.Public),
				},
			},
			{
				Hostname: "svc-private",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Namespace: "test1",
					ExportTo:  sets.New(visibility.Private),
				},
			},
			{
				Hostname: "svc-none",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Namespace: "test1",
					ExportTo:  sets.New(visibility.None),
				},
			},
			{
				Hostname: "svc-namespace",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Namespace: "test1",
					ExportTo:  sets.New(visibility.Instance("namespace")),
				},
			},
		},
		serviceInstances: []*ServiceInstance{
			{
				Endpoint: &IstioEndpoint{
					Addresses:    []string{"192.168.1.2"},
					EndpointPort: 8000,
					TLSMode:      DisabledTLSModeLabel,
				},
			},
			{
				Endpoint: &IstioEndpoint{
					Addresses:    []string{"192.168.1.3", "2001:1::3"},
					EndpointPort: 8000,
					TLSMode:      DisabledTLSModeLabel,
				},
			},
		},
	}
	m := mesh.DefaultMeshConfig()
	env.Watcher = meshwatcher.NewTestWatcher(m)
	env.Init()

	// Init a new push context
	pc := NewPushContext()
	pc.InitContext(env, nil, nil)
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
					service: sets.New(visibility.Private),
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
					service: sets.New(visibility.Private),
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
					service: sets.New(visibility.Public),
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
					ExportTo:  sets.New(visibility.Private),
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
					ExportTo:  sets.New(visibility.Private),
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
					ExportTo:  sets.New(visibility.Public),
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
					ExportTo:  sets.New(visibility.Instance("foo")),
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
					ExportTo:  sets.New(visibility.Instance("baz")),
				},
			},
			expect: false,
		},
		{
			name:        "service visible to none",
			pushContext: &PushContext{},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "bar",
					ExportTo:  sets.New(visibility.None),
				},
			},
			expect: false,
		},
		{
			name:        "service has both public visibility and none visibility",
			pushContext: &PushContext{},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "bar",
					ExportTo: sets.New(
						visibility.Public,
						visibility.None,
					),
				},
			},
			expect: true,
		},
		{
			name:        "service has both none visibility and private visibility",
			pushContext: &PushContext{},
			service: &Service{
				Attributes: ServiceAttributes{
					Namespace: "bar",
					ExportTo: sets.New(
						visibility.Private,
						visibility.None,
					),
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
	env := NewEnvironment()
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
	_, _ = configStore.Create(config.Config{
		Meta: config.Meta{
			Name:             "default",
			Namespace:        "istio-system",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{Hosts: []string{"test1/*"}},
			},
		},
	})

	env.ConfigStore = configStore
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
					ExportTo:  sets.New(visibility.Public),
				},
			},
		},
		serviceInstances: []*ServiceInstance{
			{
				Endpoint: &IstioEndpoint{
					Addresses:    []string{"192.168.1.2"},
					EndpointPort: 8000,
					TLSMode:      DisabledTLSModeLabel,
				},
			},
			{
				Endpoint: &IstioEndpoint{
					Addresses:    []string{"192.168.1.3", "2001:1::3"},
					EndpointPort: 8000,
					TLSMode:      DisabledTLSModeLabel,
				},
			},
		},
	}
	m := mesh.DefaultMeshConfig()
	env.Watcher = meshwatcher.NewTestWatcher(m)
	env.Init()

	// Init a new push context
	old := NewPushContext()
	old.InitContext(env, nil, nil)

	for _, sidecars := range old.sidecarIndex.sidecarsByNamespace {
		for _, sidecar := range sidecars {
			sidecar.initFunc()
		}
	}

	// Create a new one, copying from the old one
	// Pass a ConfigsUpdated otherwise we would just copy it directly
	newPush := NewPushContext()
	newPush.InitContext(env, old, &PushRequest{
		ConfigsUpdated: sets.Set[ConfigKey]{
			{Kind: kind.Secret}: {},
		},
	})

	for _, sidecars := range newPush.sidecarIndex.sidecarsByNamespace {
		for _, sidecar := range sidecars {
			sidecar.initFunc()
		}
	}

	// Check to ensure the update is identical to the old one
	// There is probably a better way to do this.
	diff := cmp.Diff(old, newPush,
		// Allow looking into exported fields for parts of push context
		cmp.AllowUnexported(PushContext{}, exportToDefaults{}, serviceIndex{}, virtualServiceIndex{},
			destinationRuleIndex{}, gatewayIndex{}, consolidatedDestRules{}, IstioEgressListenerWrapper{}, SidecarScope{},
			AuthenticationPolicies{}, NetworkManager{}, sidecarIndex{}, Telemetries{}, ProxyConfigs{}, ConsolidatedDestRule{},
			ClusterLocalHosts{}),
		// These are not feasible/worth comparing
		cmpopts.IgnoreTypes(sync.RWMutex{}, localServiceDiscovery{}, FakeStore{}, atomic.Bool{}, sync.Mutex{}, func() {}),
		cmpopts.IgnoreUnexported(IstioEndpoint{}),
		cmpopts.IgnoreInterfaces(struct{ mesh.Holder }{}),
		protocmp.Transform(),
	)
	if diff != "" {
		t.Fatalf("Push context had a diff after update: %v", diff)
	}
}

func TestSidecarScope(t *testing.T) {
	test.SetForTest(t, &features.ConvertSidecarScopeConcurrency, 10)
	ps := NewPushContext()
	env := &Environment{Watcher: meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})}
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
			GroupVersionKind: gvk.Sidecar,
			Name:             "foo",
			Namespace:        "default",
		},
		Spec: sidecarWithWorkloadSelector,
	}
	rootConfig := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.Sidecar,
			Name:             "global",
			Namespace:        constants.IstioSystemNamespace,
		},
		Spec: sidecarWithoutWorkloadSelector,
	}
	_, _ = configStore.Create(configWithWorkloadSelector)
	_, _ = configStore.Create(rootConfig)

	env.ConfigStore = configStore
	ps.initSidecarScopes(env)
	cases := []struct {
		proxy    *Proxy
		labels   labels.Instance
		sidecar  string
		describe string
	}{
		{
			proxy:    &Proxy{Type: SidecarProxy, ConfigNamespace: "default"},
			labels:   labels.Instance{"app": "foo"},
			sidecar:  "default/foo",
			describe: "match local sidecar",
		},
		{
			proxy:    &Proxy{Type: SidecarProxy, ConfigNamespace: "default"},
			labels:   labels.Instance{"app": "bar"},
			sidecar:  "default/global",
			describe: "no match local sidecar",
		},
		{
			proxy:    &Proxy{Type: SidecarProxy, ConfigNamespace: "nosidecar"},
			labels:   labels.Instance{"app": "bar"},
			sidecar:  "nosidecar/global",
			describe: "no sidecar",
		},
		{
			proxy:    &Proxy{Type: Router, ConfigNamespace: "istio-system"},
			labels:   labels.Instance{"app": "istio-gateway"},
			sidecar:  "istio-system/default-sidecar",
			describe: "gateway sidecar scope",
		},
	}
	for _, c := range cases {
		t.Run(c.describe, func(t *testing.T) {
			scope := ps.getSidecarScope(c.proxy, c.labels)
			if c.sidecar != scopeToSidecar(scope) {
				t.Errorf("should get sidecar %s but got %s", c.sidecar, scopeToSidecar(scope))
			}
		})
	}
}

func TestRootSidecarScopePropagation(t *testing.T) {
	rootNS := "istio-system"
	defaultNS := "default"
	otherNS := "foo"

	verifyServices := func(sidecarScopeEnabled bool, desc string, ns string, push *PushContext) {
		t.Run(desc, func(t *testing.T) {
			scScope := push.getSidecarScope(&Proxy{Type: SidecarProxy, ConfigNamespace: ns}, nil)
			services := scScope.Services()
			numSvc := 0
			svcList := []string{}
			for _, service := range services {
				svcName := service.Attributes.Name
				svcNS := service.Attributes.Namespace
				if svcNS != ns && svcNS != rootNS {
					numSvc++
				}
				svcList = append(svcList, fmt.Sprintf("%v.%v.cluster.local", svcName, svcNS))
			}
			if sidecarScopeEnabled && numSvc > 0 {
				t.Fatalf("For namespace:%v, should not see services from other ns. Instead got these services: %v", ns, svcList)
			}
		})
	}

	env := NewEnvironment()
	configStore := NewFakeStore()

	m := mesh.DefaultMeshConfig()
	env.Watcher = meshwatcher.NewTestWatcher(m)

	env.ServiceDiscovery = &localServiceDiscovery{
		services: []*Service{
			{
				Hostname: "svc1",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Name:      "svc1",
					Namespace: defaultNS,
				},
			},
			{
				Hostname: "svc2",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Name:      "svc2",
					Namespace: otherNS,
				},
			},
			{
				Hostname: "svc3",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Name:      "svc3",
					Namespace: otherNS,
				},
			},
			{
				Hostname: "svc4",
				Ports:    allPorts,
				Attributes: ServiceAttributes{
					Name:      "svc4",
					Namespace: rootNS,
				},
			},
		},
	}
	env.Init()
	globalSidecar := &networking.Sidecar{
		Egress: []*networking.IstioEgressListener{
			{
				Hosts: []string{"./*", fmt.Sprintf("%v/*", rootNS)},
			},
		},
		OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{},
	}
	rootConfig := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.Sidecar,
			Name:             "global",
			Namespace:        constants.IstioSystemNamespace,
		},
		Spec: globalSidecar,
	}

	_, _ = configStore.Create(rootConfig)
	env.ConfigStore = configStore

	testDesc := "Testing root SidecarScope for ns:%v enabled when %v is called."
	when := "createNewContext"
	newPush := NewPushContext()
	newPush.Mesh = env.Mesh()
	newPush.InitContext(env, nil, nil)
	verifyServices(true, fmt.Sprintf(testDesc, otherNS, when), otherNS, newPush)
	verifyServices(true, fmt.Sprintf(testDesc, defaultNS, when), defaultNS, newPush)

	oldPush := newPush
	newPush = NewPushContext()
	newPush.Mesh = env.Mesh()
	svcName := "svc6.foo.cluster.local"
	newPush.InitContext(env, oldPush, &PushRequest{
		ConfigsUpdated: sets.Set[ConfigKey]{
			{Kind: kind.Service, Name: svcName, Namespace: "foo"}: {},
		},
		Reason: nil,
		Full:   true,
	})
	when = "updateContext(with no changes)"
	verifyServices(true, fmt.Sprintf(testDesc, otherNS, when), otherNS, newPush)
	verifyServices(true, fmt.Sprintf(testDesc, defaultNS, when), defaultNS, newPush)
}

func TestBestEffortInferServiceMTLSMode(t *testing.T) {
	const partialNS string = "partial"
	const wholeNS string = "whole"
	ps := NewPushContext()
	env := &Environment{Watcher: meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})}
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

	env.ConfigStore = configStore
	ps.initAuthnPolicies(env)

	instancePlainText := &ServiceInstance{
		Endpoint: &IstioEndpoint{
			Addresses:    []string{"192.168.1.2"},
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

func TestSetDestinationRuleWithWorkloadSelector(t *testing.T) {
	now := time.Now()
	testhost := "httpbin.org"
	app1DestinationRule := config.Config{
		Meta: config.Meta{
			Name:              "nsRule1",
			Namespace:         "test",
			CreationTimestamp: now,
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{"test2", "."},
			WorkloadSelector: &selectorpb.WorkloadSelector{
				MatchLabels: map[string]string{"app": "app1"},
			},
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &durationpb.Duration{Seconds: 1},
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
	app2DestinationRule := config.Config{
		Meta: config.Meta{
			Name:              "nsRule2",
			Namespace:         "test",
			CreationTimestamp: now.Add(time.Second),
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{"test2", "."},
			WorkloadSelector: &selectorpb.WorkloadSelector{
				MatchLabels: map[string]string{"app": "app2"},
			},
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &durationpb.Duration{Seconds: 33},
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					Consecutive_5XxErrors: &wrapperspb.UInt32Value{Value: 3},
				},
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
			},
		},
	}
	app3DestinationRule := config.Config{
		Meta: config.Meta{
			Name:              "nsRule3",
			Namespace:         "test",
			CreationTimestamp: now.Add(time.Second),
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{"test2", "."},
			WorkloadSelector: &selectorpb.WorkloadSelector{
				MatchLabels: map[string]string{"app": "app2"},
			},
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &durationpb.Duration{Seconds: 33},
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					Consecutive_5XxErrors: &wrapperspb.UInt32Value{Value: 3},
				},
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
			},
		},
	}
	app4DestinationRule := config.Config{
		Meta: config.Meta{
			Name:              "nsRule4",
			Namespace:         "test2",
			CreationTimestamp: now.Add(time.Second),
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{"."},
			WorkloadSelector: &selectorpb.WorkloadSelector{
				MatchLabels: map[string]string{"app": "app2"},
			},
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &durationpb.Duration{Seconds: 33},
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					Consecutive_5XxErrors: &wrapperspb.UInt32Value{Value: 3},
				},
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
			},
		},
	}
	namespaceDestinationRule := config.Config{
		Meta: config.Meta{
			Name:              "nsRule5",
			Namespace:         "test",
			CreationTimestamp: now.Add(-time.Second),
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{".", "test2"},
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &durationpb.Duration{Seconds: 33},
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					Consecutive_5XxErrors: &wrapperspb.UInt32Value{Value: 3},
				},
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
			},
		},
	}
	istioSystemDestinationRule := config.Config{
		Meta: config.Meta{
			Name:              "nsRule6",
			Namespace:         "istio-system",
			CreationTimestamp: now.Add(-time.Second),
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{"*"},
			TrafficPolicy: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 33,
					},
					Tcp: &networking.ConnectionPoolSettings_TCPSettings{
						ConnectTimeout: &durationpb.Duration{Seconds: 33},
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					Consecutive_5XxErrors: &wrapperspb.UInt32Value{Value: 3},
				},
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
			},
		},
	}

	testCases := []struct {
		name                   string
		proxyNs                string
		serviceNs              string
		serviceHostname        string
		destinationRules       []config.Config
		expectedDrCount        int
		expectedDrName         []string
		expectedNamespacedFrom map[string][]types.NamespacedName
	}{
		{
			name:             "return list of DRs for specific host",
			proxyNs:          "test",
			serviceNs:        "test",
			serviceHostname:  testhost,
			destinationRules: []config.Config{app1DestinationRule, app2DestinationRule, app3DestinationRule, namespaceDestinationRule, istioSystemDestinationRule},
			expectedDrCount:  3,
			expectedDrName:   []string{app1DestinationRule.Meta.Name, app2DestinationRule.Meta.Name, namespaceDestinationRule.Meta.Name},
			expectedNamespacedFrom: map[string][]types.NamespacedName{
				app1DestinationRule.Meta.Name:      {app1DestinationRule.NamespacedName(), namespaceDestinationRule.NamespacedName()},
				app2DestinationRule.Meta.Name:      {app2DestinationRule.NamespacedName(), app3DestinationRule.NamespacedName(), namespaceDestinationRule.NamespacedName()},
				namespaceDestinationRule.Meta.Name: {namespaceDestinationRule.NamespacedName()},
			},
		},
		{
			name:                   "workload specific DR should not be exported",
			proxyNs:                "test2",
			serviceNs:              "test",
			serviceHostname:        testhost,
			destinationRules:       []config.Config{app1DestinationRule, app2DestinationRule, app3DestinationRule, namespaceDestinationRule, istioSystemDestinationRule},
			expectedDrCount:        1,
			expectedDrName:         []string{namespaceDestinationRule.Meta.Name},
			expectedNamespacedFrom: map[string][]types.NamespacedName{},
		},
		{
			name:             "rules with same workloadselector should be merged",
			proxyNs:          "test",
			serviceNs:        "test",
			serviceHostname:  testhost,
			destinationRules: []config.Config{app1DestinationRule, app2DestinationRule, app3DestinationRule, namespaceDestinationRule, istioSystemDestinationRule},
			expectedDrCount:  3,
			expectedDrName:   []string{app1DestinationRule.Meta.Name, app2DestinationRule.Meta.Name, namespaceDestinationRule.Meta.Name},
			expectedNamespacedFrom: map[string][]types.NamespacedName{
				app1DestinationRule.Meta.Name:      {app1DestinationRule.NamespacedName(), namespaceDestinationRule.NamespacedName()},
				app2DestinationRule.Meta.Name:      {app2DestinationRule.NamespacedName(), app3DestinationRule.NamespacedName(), namespaceDestinationRule.NamespacedName()},
				namespaceDestinationRule.Meta.Name: {namespaceDestinationRule.NamespacedName()},
			},
		},
		{
			name:             "DR without workloadselector in dst NS applied if src NS only contains DRs with selectors",
			proxyNs:          "test2",
			serviceNs:        "test",
			serviceHostname:  testhost,
			destinationRules: []config.Config{app4DestinationRule, namespaceDestinationRule, istioSystemDestinationRule},
			expectedDrCount:  2,
			expectedDrName:   []string{app4DestinationRule.Meta.Name, namespaceDestinationRule.Meta.Name},
			expectedNamespacedFrom: map[string][]types.NamespacedName{
				app4DestinationRule.Meta.Name:      {app4DestinationRule.NamespacedName()}, // merging across namespaces currently not supported
				namespaceDestinationRule.Meta.Name: {namespaceDestinationRule.NamespacedName()},
			},
		},
		{
			name:             "DR without workloadselector in root NS applied if src NS only contains DRs with selectors",
			proxyNs:          "test2",
			serviceNs:        "test",
			serviceHostname:  testhost,
			destinationRules: []config.Config{app4DestinationRule, istioSystemDestinationRule},
			expectedDrCount:  2,
			expectedDrName:   []string{app4DestinationRule.Meta.Name, istioSystemDestinationRule.Meta.Name},
			expectedNamespacedFrom: map[string][]types.NamespacedName{
				app4DestinationRule.Meta.Name:        {app4DestinationRule.NamespacedName()}, // merging across namespaces currently not supported
				istioSystemDestinationRule.Meta.Name: {istioSystemDestinationRule.NamespacedName()},
			},
		},
		{
			name:             "DR without workloadselector in root NS applied if src + dst NS only contain DRs with selectors",
			proxyNs:          "test2",
			serviceNs:        "test",
			serviceHostname:  testhost,
			destinationRules: []config.Config{app1DestinationRule, app2DestinationRule, app3DestinationRule, app4DestinationRule, istioSystemDestinationRule},
			expectedDrCount:  2,
			expectedDrName:   []string{app4DestinationRule.Meta.Name, istioSystemDestinationRule.Meta.Name},
			expectedNamespacedFrom: map[string][]types.NamespacedName{
				app4DestinationRule.Meta.Name:        {app4DestinationRule.NamespacedName()}, // merging across namespaces currently not supported
				istioSystemDestinationRule.Meta.Name: {istioSystemDestinationRule.NamespacedName()},
			},
		},
		{
			name:             "workload-specific DR applied if no DR without workloadselector is present at all",
			proxyNs:          "test",
			serviceNs:        "test",
			serviceHostname:  testhost,
			destinationRules: []config.Config{app1DestinationRule, app2DestinationRule, app3DestinationRule, app4DestinationRule},
			expectedDrCount:  2,
			expectedDrName:   []string{app1DestinationRule.Meta.Name, app2DestinationRule.Meta.Name},
			expectedNamespacedFrom: map[string][]types.NamespacedName{
				app1DestinationRule.Meta.Name: {app1DestinationRule.NamespacedName()},
				app2DestinationRule.Meta.Name: {app2DestinationRule.NamespacedName(), app3DestinationRule.NamespacedName()},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ps := NewPushContext()
			ps.Mesh = &meshconfig.MeshConfig{RootNamespace: "istio-system"}
			ps.setDestinationRules(tt.destinationRules)
			drList := ps.destinationRule(tt.proxyNs,
				&Service{
					Hostname: host.Name(tt.serviceHostname),
					Attributes: ServiceAttributes{
						Namespace: tt.serviceNs,
					},
				})
			if len(drList) != tt.expectedDrCount {
				t.Errorf("expected %d destinationRules for host %v got %v", tt.expectedDrCount, tt.serviceHostname, drList)
			}
			for i, dr := range drList {
				if dr.rule.Name != tt.expectedDrName[i] {
					t.Errorf("destinationRuleName expected %v got %v", tt.expectedDrName[i], dr.rule.Name)
				}
			}
			testLocal := ps.destinationRuleIndex.namespaceLocal[tt.proxyNs]
			if testLocal != nil {
				destRules := testLocal.specificDestRules
				for _, dr := range destRules[host.Name(testhost)] {

					// Check if the 'from' values match the expectedFrom map
					expectedFrom := tt.expectedNamespacedFrom[dr.rule.Meta.Name]
					if !reflect.DeepEqual(dr.from, expectedFrom) {
						t.Errorf("Unexpected 'from' value for destination rule %s. Got: %v, Expected: %v", dr.rule.NamespacedName(), dr.from, expectedFrom)
					}
				}
			}
		})
	}
}

func TestSetDestinationRuleMerging(t *testing.T) {
	ps := NewPushContext()
	ps.exportToDefaults.destinationRule = sets.New(visibility.Public)
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
	destinationRuleNamespace3 := config.Config{
		Meta: config.Meta{
			Name:      "rule3",
			Namespace: "test",
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{"istio-system"},
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

	expectedDestRules := []types.NamespacedName{
		{Namespace: "test", Name: "rule1"},
		{Namespace: "test", Name: "rule2"},
	}
	publicExpectedDestRules := []types.NamespacedName{
		{Namespace: "test", Name: "rule1"},
		{Namespace: "test", Name: "rule2"},
	}
	ps.setDestinationRules([]config.Config{destinationRuleNamespace1, destinationRuleNamespace2, destinationRuleNamespace3})
	private := ps.destinationRuleIndex.namespaceLocal["test"].specificDestRules[host.Name(testhost)]
	public := ps.destinationRuleIndex.exportedByNamespace["test"].specificDestRules[host.Name(testhost)]
	assert.Equal(t, len(private), 1)
	assert.Equal(t, len(public), 2)
	subsetsLocal := private[0].rule.Spec.(*networking.DestinationRule).Subsets
	subsetsExport := public[0].rule.Spec.(*networking.DestinationRule).Subsets
	assert.Equal(t, private[0].from, expectedDestRules)
	assert.Equal(t, public[0].from, publicExpectedDestRules)
	if len(subsetsLocal) != 4 {
		t.Errorf("want %d, but got %d", 4, len(subsetsLocal))
	}
	if len(subsetsExport) != 4 {
		t.Errorf("want %d, but got %d", 6, len(subsetsExport))
	}

	expectedDestRules = []types.NamespacedName{
		{Namespace: "test", Name: "rule3"},
	}
	subsetsExport = public[1].rule.Spec.(*networking.DestinationRule).Subsets
	assert.Equal(t, public[1].from, expectedDestRules)
	if len(subsetsExport) != 2 {
		t.Errorf("want %d, but got %d", 2, len(subsetsExport))
	}
	if !public[1].exportTo.Contains("istio-system") {
		t.Errorf("want %s, but got %v", "istio-system", public[1].exportTo)
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
			ExportTo: []string{"test2", "ns1", "test1", "newNS"},
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
	destinationRuleNamespace4Local := config.Config{
		Meta: config.Meta{
			Name:      "rule4-local",
			Namespace: "test4",
		},
		Spec: &networking.DestinationRule{
			Host:     testhost,
			ExportTo: []string{"test4"},
			Subsets: []*networking.Subset{
				{
					Name: "subset15",
				},
				{
					Name: "subset16",
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

	destinationRule5ExportToRootNamespace := config.Config{
		Meta: config.Meta{
			Name:      "rule5",
			Namespace: "test5",
		},
		Spec: &networking.DestinationRule{
			Host:     "api.test.com",
			ExportTo: []string{"istio-system"},
			Subsets: []*networking.Subset{
				{
					Name: "subset5-0",
				},
				{
					Name: "subset5-1",
				},
			},
		},
	}

	destinationRule6ExportToRootNamespace := config.Config{
		Meta: config.Meta{
			Name:      "rule6",
			Namespace: "test6",
		},
		Spec: &networking.DestinationRule{
			Host:     "api.test.com",
			ExportTo: []string{"istio-system"},
			Subsets: []*networking.Subset{
				{
					Name: "subset6-0",
				},
				{
					Name: "subset6-1",
				},
			},
		},
	}
	ps.setDestinationRules([]config.Config{
		destinationRuleNamespace1, destinationRuleNamespace2,
		destinationRuleNamespace3, destinationRuleNamespace4Local,
		destinationRuleRootNamespace, destinationRuleRootNamespaceLocal,
		destinationRuleRootNamespaceLocalWithWildcardHost1, destinationRuleRootNamespaceLocalWithWildcardHost2,
		destinationRule5ExportToRootNamespace, destinationRule6ExportToRootNamespace,
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
			proxyNs:     "newNS",
			serviceNs:   "test2",
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
			proxyNs:     "test5",
			serviceNs:   "test4",
			host:        testhost,
			wantSubsets: []string{"subset7", "subset8"},
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
		// dr in the svc ns takes effect on proxy in root ns
		{
			proxyNs:     "istio-system",
			serviceNs:   "test5",
			host:        "api.test.com",
			wantSubsets: []string{"subset5-0", "subset5-1"},
		},
		// dr in the svc ns takes effect on proxy in root ns
		{
			proxyNs:     "istio-system",
			serviceNs:   "test6",
			host:        "api.test.com",
			wantSubsets: []string{"subset6-0", "subset6-1"},
		},
		// both svc and dr namespace is not equal to proxy ns, the dr will not take effect on the proxy
		{
			proxyNs:     "istio-system",
			serviceNs:   "test7",
			host:        "api.test.com",
			wantSubsets: nil,
		},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%s-%s", tt.proxyNs, tt.serviceNs), func(t *testing.T) {
			out := ps.destinationRule(tt.proxyNs,
				&Service{
					Hostname: host.Name(tt.host),
					Attributes: ServiceAttributes{
						Namespace: tt.serviceNs,
					},
				})
			if tt.wantSubsets == nil {
				if len(out) != 0 {
					t.Fatalf("proxy in %s namespace: unexpected dr found %+v", tt.proxyNs, out)
				}
				return
			}

			if len(out) == 0 {
				t.Fatalf("proxy in %s namespace: dest rule is nil, expected subsets %+v", tt.proxyNs, tt.wantSubsets)
			}
			destRuleConfig := out[0]
			destRule := destRuleConfig.rule.Spec.(*networking.DestinationRule)
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
	env := &Environment{Watcher: meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{RootNamespace: "zzz"})}
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

	env.ConfigStore = configStore
	ps.initDefaultExportMaps()
	ps.initVirtualServices(env)

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
	testCase := func(legacy bool, ns1GatewayExpectedDestinations, ns5GatewayExpectedDestinations sets.String) {
		test.SetForTest(t, &features.FilterGatewayClusterConfig, true)
		test.SetForTest(t, &features.ScopeGatewayToNamespace, legacy)
		ps := NewPushContext()
		env := &Environment{Watcher: meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"})}
		ps.Mesh = env.Mesh()
		configStore := NewFakeStore()
		gatewayName := "ns1/gateway"
		root := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "root",
				Namespace:        "ns1",
			},
			Spec: &networking.VirtualService{
				ExportTo: []string{"*"},
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
							Name:      "delegate",
							Namespace: "ns2",
						},
					},
				},
			},
		}
		delegate := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "delegate",
				Namespace:        "ns2",
			},
			Spec: &networking.VirtualService{
				ExportTo: []string{"*"},
				Hosts:    []string{},
				Gateways: []string{gatewayName},
				Http: []*networking.HTTPRoute{
					{
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "delegate",
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
		public := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "public",
				Namespace:        "ns3",
			},
			Spec: &networking.VirtualService{
				Hosts:    []string{"*.org"},
				Gateways: []string{gatewayName},
				Http: []*networking.HTTPRoute{
					{
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "public",
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
		private := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "private",
				Namespace:        "ns1",
			},
			Spec: &networking.VirtualService{
				ExportTo: []string{".", "ns2"},
				Hosts:    []string{"*.org"},
				Gateways: []string{gatewayName},
				Http: []*networking.HTTPRoute{
					{
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "private",
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
		invisible := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "invisible",
				Namespace:        "ns5",
			},
			Spec: &networking.VirtualService{
				Hosts:    []string{"*.org"},
				Gateways: []string{"gateway", "mesh"},
				Http: []*networking.HTTPRoute{
					{
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "invisible",
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

		sourceNamespaceMatch := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "matchNs1FromDifferentNamespace",
				Namespace:        "ns5",
			},
			Spec: &networking.VirtualService{
				Hosts:    []string{"*.org"},
				Gateways: []string{gatewayName},
				Http: []*networking.HTTPRoute{
					{
						Match: []*networking.HTTPMatchRequest{
							{
								SourceNamespace: "ns1",
							},
						},
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "match-ns1",
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

		sourceNamespaceMatchWithoutGatewayNamespace := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "matchNs1-without-explicit-gateway-namespace",
				Namespace:        "ns1",
			},
			Spec: &networking.VirtualService{
				ExportTo: []string{".", "ns3"},
				Hosts:    []string{"*.org"},
				Gateways: []string{"gateway"},
				Http: []*networking.HTTPRoute{
					{
						Match: []*networking.HTTPMatchRequest{
							{
								SourceNamespace: "ns1",
							},
						},
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "match-ns1",
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
		sourceNamespaceNotMatch := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             "matchNs7",
				Namespace:        "ns7",
			},
			Spec: &networking.VirtualService{
				Hosts:    []string{"*.org"},
				Gateways: []string{gatewayName},
				Http: []*networking.HTTPRoute{
					{
						Match: []*networking.HTTPMatchRequest{
							{
								SourceNamespace: "ns7",
							},
						},
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "match-ns7",
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
		for _, c := range []config.Config{
			root,
			delegate,
			public,
			private,
			invisible,
			sourceNamespaceMatch,
			sourceNamespaceMatchWithoutGatewayNamespace,
			sourceNamespaceNotMatch,
		} {
			if _, err := configStore.Create(c); err != nil {
				t.Fatalf("could not create %v", c.Name)
			}
		}

		env.ConfigStore = configStore
		ps.initDefaultExportMaps()
		ps.initVirtualServices(env)

		t.Run("resolve shortname", func(t *testing.T) {
			rules := ps.VirtualServicesForGateway("ns1", gatewayName)
			if len(rules) != 6 {
				t.Fatalf("wanted 6 virtualservice for gateway %s, actually got %d", gatewayName, len(rules))
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
			if !reflect.DeepEqual(gotHTTPHosts, []string{"match-ns1.ns1", "private.ns1", "match-ns1.ns5", "match-ns7.ns7", "public.ns3", "delegate.ns2"}) {
				t.Errorf("got %+v", gotHTTPHosts)
			}
		})

		t.Run("destinations by gateway", func(t *testing.T) {
			got := ps.virtualServiceIndex.destinationsByGateway
			want := map[string]sets.String{
				gatewayName:   ns1GatewayExpectedDestinations,
				"ns5/gateway": ns5GatewayExpectedDestinations,
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("destinationsByGateway: got %+v", got)
			}
		})
	}

	testCase(false,
		sets.New("delegate.ns2", "public.ns3", "private.ns1", "match-ns1.ns5", "match-ns1.ns1", "match-ns7.ns7"),
		sets.New("invisible.ns5"),
	)
	testCase(true,
		sets.New("delegate.ns2", "public.ns3", "private.ns1", "match-ns1.ns5", "match-ns1.ns1"),
		sets.New("invisible.ns5"),
	)
}

func TestServiceWithExportTo(t *testing.T) {
	ps := NewPushContext()
	env := NewEnvironment()
	env.Watcher = meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{RootNamespace: "zzz"})
	ps.Mesh = env.Mesh()

	svc1 := &Service{
		Hostname: "svc1",
		Attributes: ServiceAttributes{
			Namespace: "test1",
			ExportTo:  sets.New(visibility.Private, visibility.Instance("ns1")),
		},
	}
	svc2 := &Service{
		Hostname: "svc2",
		Attributes: ServiceAttributes{
			Namespace: "test2",
			ExportTo: sets.New(
				visibility.Instance("test1"),
				visibility.Instance("ns1"),
				visibility.Instance("test2"),
			),
		},
	}
	svc3 := &Service{
		Hostname: "svc3",
		Attributes: ServiceAttributes{
			Namespace: "test3",
			ExportTo: sets.New(
				visibility.Instance("test1"),
				visibility.Public,
				visibility.Instance("test2"),
			),
		},
	}
	svc4 := &Service{
		Hostname: "svc4",
		Attributes: ServiceAttributes{
			Namespace:       "test4",
			ServiceRegistry: provider.External,
		},
	}
	svc4_1 := &Service{
		Hostname: "svc4",
		Attributes: ServiceAttributes{
			Namespace:       "test4",
			ServiceRegistry: provider.External,
		},
	}
	// kubernetes service will override non kubernetes
	svc4_2 := &Service{
		Hostname: "svc4",
		Attributes: ServiceAttributes{
			Namespace:       "test4",
			ServiceRegistry: provider.Kubernetes,
		},
	}
	env.ServiceDiscovery = &localServiceDiscovery{
		services: []*Service{svc1, svc2, svc3, svc4, svc4_1, svc4_2},
	}
	ps.initDefaultExportMaps()
	ps.initServiceRegistry(env, nil)
	assert.Equal(t, ps.ServiceIndex.HostnameAndNamespace[svc4.Hostname][svc4.Attributes.Namespace].Attributes.ServiceRegistry, provider.Kubernetes)
	cases := []struct {
		proxyNs   string
		wantHosts []string
	}{
		{
			proxyNs:   "test1",
			wantHosts: []string{"svc1", "svc2", "svc3", "svc4", "svc4", "svc4"},
		},
		{
			proxyNs:   "test2",
			wantHosts: []string{"svc2", "svc3", "svc4", "svc4", "svc4"},
		},
		{
			proxyNs:   "ns1",
			wantHosts: []string{"svc1", "svc2", "svc3", "svc4", "svc4", "svc4"},
		},
		{
			proxyNs:   "random",
			wantHosts: []string{"svc3", "svc4", "svc4", "svc4"},
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

func TestInstancesByPort(t *testing.T) {
	ps := NewPushContext()
	env := NewEnvironment()
	env.Watcher = meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{RootNamespace: "zzz"})
	ps.Mesh = env.Mesh()

	// Test the Service Entry merge with same host with different generates
	// correct instances by port.
	svc5_1 := &Service{
		Hostname: "svc5",
		Attributes: ServiceAttributes{
			Namespace:       "test5",
			ServiceRegistry: provider.External,
			ExportTo: sets.New(
				visibility.Instance("test5"),
			),
		},
		Ports:      port7000,
		Resolution: DNSLB,
	}
	svc5_2 := &Service{
		Hostname: "svc5",
		Attributes: ServiceAttributes{
			Namespace:       "test5",
			ServiceRegistry: provider.External,
			ExportTo: sets.New(
				visibility.Instance("test5"),
			),
		},
		Ports:      port8000,
		Resolution: DNSLB,
	}

	env.ServiceDiscovery = &localServiceDiscovery{
		services: []*Service{svc5_1, svc5_2},
	}

	env.EndpointIndex.shardsBySvc = map[string]map[string]*EndpointShards{
		svc5_1.Hostname.String(): {
			svc5_1.Attributes.Namespace: {
				Shards: map[ShardKey][]*IstioEndpoint{
					{Cluster: "Kubernetes", Provider: provider.External}: {
						&IstioEndpoint{
							Addresses:       []string{"1.1.1.1"},
							EndpointPort:    7000,
							ServicePortName: "uds",
						},
						&IstioEndpoint{
							Addresses:       []string{"1.1.1.2", "2001:1::2"},
							EndpointPort:    8000,
							ServicePortName: "uds",
						},
					},
				},
			},
		},
	}

	ps.initServiceRegistry(env, nil)
	instancesByPort := ps.ServiceIndex.instancesByPort[svc5_1.Key()]
	assert.Equal(t, len(instancesByPort), 2)
}

func TestGetHostsFromMeshConfig(t *testing.T) {
	ps := NewPushContext()
	env := NewEnvironment()
	env.Watcher = meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{
		RootNamespace: "istio-system",
		ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "otel",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls{
					EnvoyOtelAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
						Service: "otel.foo.svc.cluster.local",
						Port:    9811,
					},
				},
			},
			{
				Name: "otel-ns-scoped",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls{
					EnvoyOtelAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
						Service: "bar/otel.example.com",
						Port:    9811,
					},
				},
			},
			{
				Name: "otel-missing-ns-scoped",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls{
					EnvoyOtelAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
						Service: "wrong-ns/otel-wrong.example.com",
						Port:    9811,
					},
				},
			},
		},
		DefaultProviders: &meshconfig.MeshConfig_DefaultProviders{
			AccessLogging: []string{"otel"},
		},
	})
	ps.Mesh = env.Mesh()
	configStore := NewFakeStore()
	gatewayName := "ns1/gateway"

	vs1 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
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
			GroupVersionKind: gvk.VirtualService,
			Name:             "vs2",
			Namespace:        "ns2",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{},
			Gateways: []string{gatewayName},
			Http: []*networking.HTTPRoute{
				{
					Route: []*networking.HTTPRouteDestination{},
				},
			},
		},
	}
	ef := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.EnvoyFilter,
			Annotations:      map[string]string{"envoyfilter.istio.io/referenced-services": "envoyfilter.example.com"},
			Name:             "ef",
			Namespace:        "istio-system",
		},
		Spec: &networking.EnvoyFilter{},
	}

	for _, c := range []config.Config{vs1, vs2, ef} {
		if _, err := configStore.Create(c); err != nil {
			t.Fatalf("could not create %v", c.Name)
		}
	}

	env.ConfigStore = configStore
	test.SetForTest(t, &features.FilterGatewayClusterConfig, true)
	ps.initTelemetry(env)
	ps.initDefaultExportMaps()
	ps.initVirtualServices(env)
	env.ServiceDiscovery = &localServiceDiscovery{
		services: []*Service{
			{
				Hostname:   "otel.foo.svc.cluster.local",
				Attributes: ServiceAttributes{Namespace: "foo"},
			},
			{
				Hostname:   "otel.example.com",
				Attributes: ServiceAttributes{Namespace: "bar"},
			},
			{
				Hostname:   "otel-wrong.example.com",
				Attributes: ServiceAttributes{Namespace: "some-ns"},
			},
			{
				Hostname:   "envoyfilter.example.com",
				Attributes: ServiceAttributes{Namespace: "some-ns"},
			},
		},
	}
	ps.initDefaultExportMaps()
	ps.initEnvoyFilters(env, nil, nil)
	ps.initServiceRegistry(env, nil)
	proxy := &Proxy{Type: Router}
	proxy.SetSidecarScope(ps)
	proxy.SetGatewaysForProxy(ps)
	patches := ps.EnvoyFilters(proxy)
	got := sets.New(slices.Map(ps.GatewayServices(proxy, patches), func(e *Service) string {
		return e.Hostname.String()
	})...)
	// Should match 2 of the 3 providers; one has a mismatched namespace though
	assert.Equal(t, got, sets.New("otel.foo.svc.cluster.local", "otel.example.com", "envoyfilter.example.com"))
}

func TestWellKnownProvidersCount(t *testing.T) {
	msg := &meshconfig.MeshConfig_ExtensionProvider{}
	pb := msg.ProtoReflect()
	md := pb.Descriptor()

	found := sets.New[string]()
	for i := 0; i < md.Oneofs().Get(0).Fields().Len(); i++ {
		found.Insert(string(md.Oneofs().Get(0).Fields().Get(i).Name()))
	}
	// If this fails, there is a provider added that we have not handled.
	// DO NOT JUST
	assert.Equal(t, found, wellknownProviders)
}

// TestGetHostsFromMeshConfigExhaustiveness exhaustiveness check of `getHostsFromMeshConfig`
// Once some one add a new `Provider` in api, we should update `wellknownProviders` and
// implements of `getHostsFromMeshConfig`
func TestGetHostsFromMeshConfigExhaustiveness(t *testing.T) {
	AssertProvidersHandled(addHostsFromMeshConfigProvidersHandled)
	unexpectedProviders := make([]string, 0)
	msg := &meshconfig.MeshConfig_ExtensionProvider{}
	pb := msg.ProtoReflect()
	md := pb.Descriptor()

	// We only consider ones with `service` field
	wellknownProviders := wellknownProviders.Copy().DeleteAll("prometheus", "stackdriver", "envoy_file_access_log")
	of := md.Oneofs().Get(0)
	for i := 0; i < of.Fields().Len(); i++ {
		o := of.Fields().Get(i)
		if o.Message().Fields().ByName("service") != nil {
			n := string(o.Name())
			if _, ok := wellknownProviders[n]; ok {
				delete(wellknownProviders, n)
			} else {
				unexpectedProviders = append(unexpectedProviders, n)
			}
		}
	}

	if len(wellknownProviders) != 0 || len(unexpectedProviders) != 0 {
		t.Errorf("unexpected provider not implemented in getHostsFromMeshConfig: %v, %v", wellknownProviders, unexpectedProviders)
		t.Fail()
	}
}

var _ ServiceDiscovery = &localServiceDiscovery{}

// localServiceDiscovery is an in-memory ServiceDiscovery with mock services
type localServiceDiscovery struct {
	services         []*Service
	serviceInstances []*ServiceInstance

	NoopAmbientIndexes
	NetworkGatewaysHandler
}

var _ ServiceDiscovery = &localServiceDiscovery{}

func (l *localServiceDiscovery) Services() []*Service {
	return l.services
}

func (l *localServiceDiscovery) GetService(host.Name) *Service {
	panic("implement me")
}

func (l *localServiceDiscovery) GetProxyServiceTargets(*Proxy) []ServiceTarget {
	panic("implement me")
}

func (l *localServiceDiscovery) GetProxyWorkloadLabels(*Proxy) labels.Instance {
	panic("implement me")
}

func (l *localServiceDiscovery) GetIstioServiceAccounts(*Service) []string {
	return nil
}

func (l *localServiceDiscovery) NetworkGateways() []NetworkGateway {
	// TODO implement fromRegistry logic from kube controller if needed
	return nil
}

func (l *localServiceDiscovery) MCSServices() []MCSServiceInfo {
	return nil
}

func TestResolveServiceAliases(t *testing.T) {
	type service struct {
		Name         host.Name
		Aliases      host.Names
		ExternalName string
	}
	tests := []struct {
		name   string
		input  []service
		output []service
	}{
		{
			name:   "no aliases",
			input:  []service{{Name: "test"}},
			output: []service{{Name: "test"}},
		},
		{
			name: "simple alias",
			input: []service{
				{Name: "concrete"},
				{Name: "alias", ExternalName: "concrete"},
			},
			output: []service{
				{Name: "concrete", Aliases: host.Names{"alias"}},
				{Name: "alias", ExternalName: "concrete"},
			},
		},
		{
			name: "multiple alias",
			input: []service{
				{Name: "concrete"},
				{Name: "alias1", ExternalName: "concrete"},
				{Name: "alias2", ExternalName: "concrete"},
			},
			output: []service{
				{Name: "concrete", Aliases: host.Names{"alias1", "alias2"}},
				{Name: "alias1", ExternalName: "concrete"},
				{Name: "alias2", ExternalName: "concrete"},
			},
		},
		{
			name: "chained alias",
			input: []service{
				{Name: "concrete"},
				{Name: "alias1", ExternalName: "alias2"},
				{Name: "alias2", ExternalName: "concrete"},
			},
			output: []service{
				{Name: "concrete", Aliases: host.Names{"alias1", "alias2"}},
				{Name: "alias1", ExternalName: "alias2"},
				{Name: "alias2", ExternalName: "concrete"},
			},
		},
		{
			name: "looping alias",
			input: []service{
				{Name: "alias1", ExternalName: "alias2"},
				{Name: "alias2", ExternalName: "alias1"},
			},
			output: []service{
				{Name: "alias1", ExternalName: "alias2"},
				{Name: "alias2", ExternalName: "alias1"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inps := slices.Map(tt.input, func(e service) *Service {
				resolution := ClientSideLB
				if e.ExternalName != "" {
					resolution = Alias
				}
				return &Service{
					Resolution: resolution,
					Attributes: ServiceAttributes{
						K8sAttributes: K8sAttributes{ExternalName: e.ExternalName},
					},
					Hostname: e.Name,
				}
			})
			resolveServiceAliases(inps, nil)
			out := slices.Map(inps, func(e *Service) service {
				return service{
					Name: e.Hostname,
					Aliases: slices.Map(e.Attributes.Aliases, func(e NamespacedHostname) host.Name {
						return e.Hostname
					}),
					ExternalName: e.Attributes.K8sAttributes.ExternalName,
				}
			})
			assert.Equal(t, tt.output, out)
		})
	}
}

func BenchmarkInitServiceAccounts(b *testing.B) {
	ps := NewPushContext()
	index := NewEndpointIndex(DisabledCache{})
	env := &Environment{EndpointIndex: index}
	ps.Mesh = &meshconfig.MeshConfig{TrustDomainAliases: []string{"td1", "td2"}}

	services := []*Service{
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
				ExportTo:  sets.New(visibility.Public),
			},
		},
		{
			Hostname: "svc-private",
			Ports:    allPorts,
			Attributes: ServiceAttributes{
				Namespace: "test1",
				ExportTo:  sets.New(visibility.Private),
			},
		},
		{
			Hostname: "svc-none",
			Ports:    allPorts,
			Attributes: ServiceAttributes{
				Namespace: "test1",
				ExportTo:  sets.New(visibility.None),
			},
		},
		{
			Hostname: "svc-namespace",
			Ports:    allPorts,
			Attributes: ServiceAttributes{
				Namespace: "test1",
				ExportTo:  sets.New(visibility.Instance("namespace")),
			},
		},
	}

	for _, svc := range services {
		if index.shardsBySvc[string(svc.Hostname)] == nil {
			index.shardsBySvc[string(svc.Hostname)] = map[string]*EndpointShards{}
		}
		index.shardsBySvc[string(svc.Hostname)][svc.Attributes.Namespace] = &EndpointShards{
			ServiceAccounts: sets.New("spiffe://cluster.local/ns/def/sa/sa1", "spiffe://cluster.local/ns/def/sa/sa2"),
		}
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		ps.initServiceAccounts(env, services)
	}
}
