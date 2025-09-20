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
	"testing"
	"time"

	fuzz "github.com/google/gofuzz"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
	"k8s.io/apimachinery/pkg/types"
)

const wildcardIP = "0.0.0.0"

func TestMergeVirtualServices(t *testing.T) {
	independentVs := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "virtual-service",
			Namespace:        "default",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"example.org"},
			Gateways: []string{"gateway"},
			Http: []*networking.HTTPRoute{
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "example.org",
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

	rootVs := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "root-vs",
			Namespace:        "istio-system",
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
						Name:      "productpage-vs",
						Namespace: "default",
					},
				},
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "example.org",
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

	defaultVs := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "default-vs",
			Namespace:        "default",
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
						Name: "productpage-vs",
					},
				},
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "example.org",
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

	oneRoot := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "root-vs",
			Namespace:        "istio-system",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"gateway"},
			Http: []*networking.HTTPRoute{
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "example.org",
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

	createDelegateVs := func(name, namespace string, exportTo []string) config.Config {
		return config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             name,
				Namespace:        namespace,
			},
			Spec: &networking.VirtualService{
				Hosts:    []string{},
				Gateways: []string{"gateway"},
				ExportTo: exportTo,
				Http: []*networking.HTTPRoute{
					{
						Match: []*networking.HTTPMatchRequest{
							{
								Uri: &networking.StringMatch{
									MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v1"},
								},
							},
						},
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "productpage.org",
									Port: &networking.PortSelector{
										Number: 80,
									},
									Subset: "v1",
								},
							},
						},
					},
					{
						Match: []*networking.HTTPMatchRequest{
							{
								Uri: &networking.StringMatch{
									MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
								},
							},
						},
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "productpage.org",
									Port: &networking.PortSelector{
										Number: 80,
									},
									Subset: "v2",
								},
							},
						},
					},
					{
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "productpage.org",
									Port: &networking.PortSelector{
										Number: 80,
									},
									Subset: "v3",
								},
							},
						},
					},
				},
			},
		}
	}

	delegateVs := createDelegateVs("productpage-vs", "default", []string{"istio-system"})
	delegateVsExportedToAll := createDelegateVs("productpage-vs", "default", []string{})

	delegateVsNotExported := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "productpage-vs",
			Namespace:        "default2",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{},
			Gateways: []string{"gateway"},
			ExportTo: []string{"."},
		},
	}

	mergedVs := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "root-vs",
			Namespace:        "istio-system",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"gateway"},
			Http: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v2",
							},
						},
					},
				},
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
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v3",
							},
						},
					},
				},
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "example.org",
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

	mergedVsInDefault := mergedVs.DeepCopy()
	mergedVsInDefault.Name = "default-vs"
	mergedVsInDefault.Namespace = "default"

	// invalid delegate, match condition conflicts with root
	delegateVs2 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "productpage-vs",
			Namespace:        "default",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{},
			Gateways: []string{"gateway"},
			Http: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v2",
							},
						},
					},
				},
				{
					// mismatch, this route will be ignored
					Match: []*networking.HTTPMatchRequest{
						{
							Name: "mismatch",
							Uri: &networking.StringMatch{
								// conflicts with root's HTTPMatchRequest
								MatchType: &networking.StringMatch_Prefix{Prefix: "/mis-match/path"},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v3",
							},
						},
					},
				},
			},
		},
	}

	mergedVs2 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "root-vs",
			Namespace:        "istio-system",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"gateway"},
			Http: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v2",
							},
						},
					},
				},
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "example.org",
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

	// multiple routes delegate to one single sub VS
	multiRoutes := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "root-vs",
			Namespace:        "istio-system",
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
					},
					Delegate: &networking.Delegate{
						Name:      "productpage-vs",
						Namespace: "default",
					},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/legacy/path"},
							},
						},
					},
					Rewrite: &networking.HTTPRewrite{
						Uri: "/productpage",
					},
					Delegate: &networking.Delegate{
						Name:      "productpage-vs",
						Namespace: "default",
					},
				},
			},
		},
	}

	singleDelegate := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "productpage-vs",
			Namespace:        "default",
		},
		Spec: &networking.VirtualService{
			Hosts:    []string{},
			Gateways: []string{"gateway"},
			Http: []*networking.HTTPRoute{
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
			},
		},
	}

	mergedVs3 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "root-vs",
			Namespace:        "istio-system",
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
					},
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/legacy/path"},
							},
						},
					},
					Rewrite: &networking.HTTPRewrite{
						Uri: "/productpage",
					},
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: "productpage.org",
								Port: &networking.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
			},
		},
	}

	cases := []struct {
		name                    string
		virtualServices         []config.Config
		expectedVirtualServices []config.Config
		defaultExportTo         sets.Set[visibility.Instance]
	}{
		{
			name:                    "one independent vs",
			virtualServices:         []config.Config{independentVs},
			expectedVirtualServices: []config.Config{independentVs},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "one root vs",
			virtualServices:         []config.Config{rootVs},
			expectedVirtualServices: []config.Config{oneRoot},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "one delegate vs",
			virtualServices:         []config.Config{delegateVs},
			expectedVirtualServices: []config.Config{},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root and delegate vs",
			virtualServices:         []config.Config{rootVs.DeepCopy(), delegateVs},
			expectedVirtualServices: []config.Config{mergedVs},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root and conflicted delegate vs",
			virtualServices:         []config.Config{rootVs.DeepCopy(), delegateVs2},
			expectedVirtualServices: []config.Config{mergedVs2},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "multiple routes delegate to one",
			virtualServices:         []config.Config{multiRoutes.DeepCopy(), singleDelegate},
			expectedVirtualServices: []config.Config{mergedVs3},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root not specify delegate namespace default public",
			virtualServices:         []config.Config{defaultVs.DeepCopy(), delegateVsExportedToAll},
			expectedVirtualServices: []config.Config{mergedVsInDefault},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "delegate not exported to root vs namespace default public",
			virtualServices:         []config.Config{rootVs, delegateVsNotExported},
			expectedVirtualServices: []config.Config{oneRoot},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root not specify delegate namespace default private",
			virtualServices:         []config.Config{defaultVs.DeepCopy(), delegateVsExportedToAll},
			expectedVirtualServices: []config.Config{mergedVsInDefault},
			defaultExportTo:         sets.New(visibility.Private),
		},
		{
			name:                    "delegate not exported to root vs namespace default private",
			virtualServices:         []config.Config{rootVs, delegateVsNotExported},
			expectedVirtualServices: []config.Config{oneRoot},
			defaultExportTo:         sets.New(visibility.Private),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := mergeVirtualServicesIfNeeded(tc.virtualServices, tc.defaultExportTo)
			assert.Equal(t, got, tc.expectedVirtualServices)
		})
	}

	t.Run("test merge order", func(t *testing.T) {
		root := rootVs.DeepCopy()
		delegate := delegateVs.DeepCopy()
		normal := independentVs.DeepCopy()

		// make sorting results predictable.
		t0 := time.Now()
		root.CreationTimestamp = t0.Add(1)
		delegate.CreationTimestamp = t0.Add(2)
		normal.CreationTimestamp = t0.Add(3)

		checkOrder := func(got []config.Config, _ map[ConfigKey][]ConfigKey) {
			gotOrder := make([]string, 0, len(got))
			for _, c := range got {
				gotOrder = append(gotOrder, fmt.Sprintf("%s/%s", c.Namespace, c.Name))
			}
			wantOrder := []string{"istio-system/root-vs", "default/virtual-service"}
			assert.Equal(t, gotOrder, wantOrder)
		}

		vses := []config.Config{root, delegate, normal}
		checkOrder(mergeVirtualServicesIfNeeded(vses, sets.New(visibility.Public)))

		vses = []config.Config{normal, delegate, root}
		checkOrder(mergeVirtualServicesIfNeeded(vses, sets.New(visibility.Public)))
	})
}

func TestMergeHttpRoutes(t *testing.T) {
	dstV1 := &networking.Destination{
		Host: "productpage.org",
		Port: &networking.PortSelector{
			Number: 80,
		},
		Subset: "v1",
	}
	dstV2 := &networking.Destination{
		Host: "productpage.org",
		Port: &networking.PortSelector{
			Number: 80,
		},
		Subset: "v2",
	}
	dstV3 := &networking.Destination{
		Host: "productpage.org",
		Port: &networking.PortSelector{
			Number: 80,
		},
		Subset: "v3",
	}
	dstMirrorV1 := dstV1.DeepCopy()
	dstMirrorV1.Host = "productpage-mirror.org"
	dstMirrorV2 := dstV2.DeepCopy()
	dstMirrorV2.Host = "productpage-mirror.org"
	dstMirrorV3 := dstV3.DeepCopy()
	dstMirrorV3.Host = "productpage-mirror.org"

	cases := []struct {
		name     string
		root     *networking.HTTPRoute
		delegate []*networking.HTTPRoute
		expected []*networking.HTTPRoute
	}{
		{
			name: "root catch all",
			root: &networking.HTTPRoute{
				Match:   nil, // catch all
				Timeout: &durationpb.Duration{Seconds: 10},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"istio": "test",
						},
						Remove: []string{"trace-id"},
					},
				},
				Delegate: &networking.Delegate{
					Name:      "delegate",
					Namespace: "default",
				},
			},
			delegate: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v1"},
								},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{{Destination: dstV1}},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
						},
						{
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
							QueryParams: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{{Destination: dstV2}},
				},
			},
			expected: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v1"},
								},
							},
						},
					},
					Route:   []*networking.HTTPRouteDestination{{Destination: dstV1}},
					Timeout: &durationpb.Duration{Seconds: 10},
					Headers: &networking.Headers{
						Request: &networking.Headers_HeaderOperations{
							Add: map[string]string{
								"istio": "test",
							},
							Remove: []string{"trace-id"},
						},
					},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
						},
						{
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
							QueryParams: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
						},
					},
					Route:   []*networking.HTTPRouteDestination{{Destination: dstV2}},
					Timeout: &durationpb.Duration{Seconds: 10},
					Headers: &networking.Headers{
						Request: &networking.Headers_HeaderOperations{
							Add: map[string]string{
								"istio": "test",
							},
							Remove: []string{"trace-id"},
						},
					},
				},
			},
		},
		{
			name: "delegate with empty match",
			root: &networking.HTTPRoute{
				Match: []*networking.HTTPMatchRequest{
					{
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
						},
						Port: 8080,
					},
				},
				Delegate: &networking.Delegate{
					Name:      "delegate",
					Namespace: "default",
				},
			},
			delegate: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v1"},
								},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{{Destination: dstV1}},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
						},
						{
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
							QueryParams: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
						},
					},
					Route: []*networking.HTTPRouteDestination{{Destination: dstV2}},
				},
				{
					// default route to v3
					Route: []*networking.HTTPRouteDestination{{Destination: dstV3}},
				},
			},
			expected: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v1"},
								},
							},
							Port: 8080,
						},
					},
					Route: []*networking.HTTPRouteDestination{{Destination: dstV1}},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
							Port: 8080,
						},
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
							},
							Headers: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
							QueryParams: map[string]*networking.StringMatch{
								"version": {
									MatchType: &networking.StringMatch_Exact{Exact: "v2"},
								},
							},
							Port: 8080,
						},
					},
					Route: []*networking.HTTPRouteDestination{{Destination: dstV2}},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
							},
							Port: 8080,
						},
					},
					// default route to v3
					Route: []*networking.HTTPRouteDestination{{Destination: dstV3}},
				},
			},
		},
		{
			name: "delegate with mirrors",
			root: &networking.HTTPRoute{
				Match:   nil,
				Mirrors: []*networking.HTTPMirrorPolicy{{Destination: dstMirrorV3}},
				Delegate: &networking.Delegate{
					Name:      "delegate",
					Namespace: "default",
				},
			},
			delegate: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
						},
					},
					Mirrors: []*networking.HTTPMirrorPolicy{{Destination: dstMirrorV1}},
					Route:   []*networking.HTTPRouteDestination{{Destination: dstV1}},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
						},
					},
					Mirrors: []*networking.HTTPMirrorPolicy{{Destination: dstMirrorV2}},
					Route:   []*networking.HTTPRouteDestination{{Destination: dstV2}},
				},
				{
					// default route to v3 with no specified mirrors
					Route: []*networking.HTTPRouteDestination{{Destination: dstV3}},
				},
			},
			expected: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
						},
					},
					Mirrors: []*networking.HTTPMirrorPolicy{{Destination: dstMirrorV1}},
					Route:   []*networking.HTTPRouteDestination{{Destination: dstV1}},
				},
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
						},
					},
					Mirrors: []*networking.HTTPMirrorPolicy{{Destination: dstMirrorV2}},
					Route:   []*networking.HTTPRouteDestination{{Destination: dstV2}},
				},
				{
					// default route to v3
					Mirrors: []*networking.HTTPMirrorPolicy{{Destination: dstMirrorV3}},
					Route:   []*networking.HTTPRouteDestination{{Destination: dstV3}},
				},
			},
		},
		{
			name: "multiple header merge",
			root: &networking.HTTPRoute{
				Match: []*networking.HTTPMatchRequest{
					{
						Headers: map[string]*networking.StringMatch{
							"header1": {
								MatchType: &networking.StringMatch_Regex{
									Regex: "regex",
								},
							},
						},
					},
					{
						Headers: map[string]*networking.StringMatch{
							"header2": {
								MatchType: &networking.StringMatch_Exact{
									Exact: "exact",
								},
							},
						},
					},
				},
				Delegate: &networking.Delegate{
					Name:      "delegate",
					Namespace: "default",
				},
			},
			delegate: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{
									Prefix: "/",
								},
							},
						},
					},
				},
			},
			expected: []*networking.HTTPRoute{
				{
					Match: []*networking.HTTPMatchRequest{
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{
									Prefix: "/",
								},
							},
							Headers: map[string]*networking.StringMatch{
								"header1": {
									MatchType: &networking.StringMatch_Regex{
										Regex: "regex",
									},
								},
							},
						},
						{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Prefix{
									Prefix: "/",
								},
							},
							Headers: map[string]*networking.StringMatch{
								"header2": {
									MatchType: &networking.StringMatch_Exact{
										Exact: "exact",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeHTTPRoutes(tc.root, tc.delegate)
			assert.Equal(t, got, tc.expected)
		})
	}
}

func TestMergeHTTPMatchRequests(t *testing.T) {
	cases := []struct {
		name     string
		root     []*networking.HTTPMatchRequest
		delegate []*networking.HTTPMatchRequest
		expected []*networking.HTTPMatchRequest
	}{
		{
			name: "url match",
			root: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
				},
			},
			expected: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
				},
			},
		},
		{
			name: "url regex noconflict",
			root: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Regex{Regex: "^/productpage"},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{},
			},
			expected: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Regex{Regex: "^/productpage"},
					},
				},
			},
		},
		{
			name: "url regex conflict",
			root: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Regex{Regex: "^/productpage"},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
					},
				},
			},
			expected: nil,
		},
		{
			name: "multi url match",
			root: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
					},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: "/reviews"},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/reviews/v1"},
					},
				},
			},
			expected: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/reviews/v1"},
					},
				},
			},
		},
		{
			name: "url mismatch",
			root: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/reviews"},
					},
				},
			},
			expected: nil,
		},
		{
			name: "url match",
			root: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
				},
				{
					Uri: &networking.StringMatch{
						// conflicts
						MatchType: &networking.StringMatch_Exact{Exact: "/reviews"},
					},
				},
			},
			expected: nil,
		},
		{
			name: "headers",
			root: []*networking.HTTPMatchRequest{
				{
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "h1"},
						},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{},
			expected: []*networking.HTTPMatchRequest{
				{
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "h1"},
						},
					},
				},
			},
		},
		{
			name: "headers conflict",
			root: []*networking.HTTPMatchRequest{
				{
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "h1"},
						},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "h2"},
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "headers",
			root: []*networking.HTTPMatchRequest{
				{
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "h1"},
						},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Headers: map[string]*networking.StringMatch{
						"header-2": {
							MatchType: &networking.StringMatch_Exact{Exact: "h2"},
						},
					},
				},
			},
			expected: []*networking.HTTPMatchRequest{
				{
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "h1"},
						},
						"header-2": {
							MatchType: &networking.StringMatch_Exact{Exact: "h2"},
						},
					},
				},
			},
		},
		{
			name: "complicated merge",
			root: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
					},
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "h1"},
						},
					},
					Port: 8080,
					Authority: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "productpage.com"},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v2"},
					},
				},
			},
			expected: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "h1"},
						},
					},
					Port: 8080,
					Authority: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "productpage.com"},
					},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v2"},
					},
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "h1"},
						},
					},
					Port: 8080,
					Authority: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "productpage.com"},
					},
				},
			},
		},
		{
			name: "conflicted merge",
			root: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
					},
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "h1"},
						},
					},
					Port: 8080,
					Authority: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "productpage.com"},
					},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v2"},
					},
					Port: 9090, // conflicts
				},
			},
			expected: nil,
		},
		{
			name: "gateway merge",
			root: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
					},
					Gateways: []string{"ingress-gateway", "mesh"},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
					Gateways: []string{"ingress-gateway"},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v2"},
					},
					Gateways: []string{"mesh"},
				},
			},
			expected: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
					Gateways: []string{"ingress-gateway"},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v2"},
					},
					Gateways: []string{"mesh"},
				},
			},
		},
		{
			name: "gateway conflicted merge",
			root: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
					},
					Gateways: []string{"ingress-gateway"},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v1"},
					},
					Gateways: []string{"ingress-gateway"},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v2"},
					},
					Gateways: []string{"mesh"},
				},
			},
			expected: nil,
		},
		{
			name: "source labels merge",
			root: []*networking.HTTPMatchRequest{
				{
					SourceLabels: map[string]string{"app": "test"},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					SourceLabels: map[string]string{"version": "v1"},
				},
			},
			expected: []*networking.HTTPMatchRequest{
				{
					SourceLabels: map[string]string{"app": "test", "version": "v1"},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.delegate = config.DeepCopy(tc.delegate).([]*networking.HTTPMatchRequest)
			got, _ := mergeHTTPMatchRequests(tc.root, tc.delegate)
			assert.Equal(t, got, tc.expected)
		})
	}
}

func TestHasConflict(t *testing.T) {
	cases := []struct {
		name     string
		root     *networking.HTTPMatchRequest
		leaf     *networking.HTTPMatchRequest
		expected bool
	}{
		{
			name: "regex uri",
			root: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{Regex: "^/productpage"},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
				},
			},
			expected: true,
		},
		{
			name: "regex uri in root and delegate does not have uri",
			root: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{Regex: "^/productpage"},
				},
			},
			leaf:     &networking.HTTPMatchRequest{},
			expected: false,
		},
		{
			name: "regex uri in delegate and root does not have uri",
			root: &networking.HTTPMatchRequest{},
			leaf: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{Regex: "^/productpage"},
				},
			},
			expected: false,
		},
		{
			name: "regex uri in root and delegate has conflicting uri match",
			root: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{Regex: "^/productpage"},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
				},
			},
			expected: true,
		},
		{
			name: "regex uri in delegate and root has conflicting uri match",
			root: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{Regex: "^/productpage"},
				},
			},
			expected: true,
		},
		{
			name: "match uri",
			root: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
				},
			},
			expected: false,
		},
		{
			name: "mismatch uri",
			root: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage"},
				},
			},
			expected: true,
		},
		{
			name: "match uri",
			root: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: "/productpage/v2"},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v2"},
				},
			},
			expected: false,
		},
		{
			name: "headers not equal",
			root: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Exact{Exact: "h1"},
					},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Exact{Exact: "h2"},
					},
				},
			},
			expected: true,
		},
		{
			name: "headers equal",
			root: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Exact{Exact: "h1"},
					},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Exact{Exact: "h1"},
					},
				},
			},
			expected: false,
		},
		{
			name: "headers match",
			root: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Prefix{Prefix: "h1"},
					},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Exact{Exact: "h1-v1"},
					},
				},
			},
			expected: false,
		},
		{
			name: "headers mismatch",
			root: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Prefix{Prefix: "h1"},
					},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Exact{Exact: "h2"},
					},
				},
			},
			expected: true,
		},
		{
			name: "headers prefix mismatch",
			root: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Prefix{Prefix: "h1"},
					},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Prefix{Prefix: "h2"},
					},
				},
			},
			expected: true,
		},
		{
			name: "diff headers",
			root: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Exact{Exact: "h1"},
					},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				Headers: map[string]*networking.StringMatch{
					"header-2": {
						MatchType: &networking.StringMatch_Exact{Exact: "h2"},
					},
				},
			},
			expected: false,
		},
		{
			name: "withoutHeaders mismatch",
			root: &networking.HTTPMatchRequest{
				WithoutHeaders: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Prefix{Prefix: "h1"},
					},
				},
			},
			leaf: &networking.HTTPMatchRequest{
				WithoutHeaders: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Exact{Exact: "h2"},
					},
				},
			},
			expected: true,
		},
		{
			name: "port",
			root: &networking.HTTPMatchRequest{
				Port: 0,
			},
			leaf: &networking.HTTPMatchRequest{
				Port: 8080,
			},
			expected: false,
		},
		{
			name: "port",
			root: &networking.HTTPMatchRequest{
				Port: 8080,
			},
			leaf: &networking.HTTPMatchRequest{
				Port: 0,
			},
			expected: false,
		},
		{
			name: "port",
			root: &networking.HTTPMatchRequest{
				Port: 8080,
			},
			leaf: &networking.HTTPMatchRequest{
				Port: 8090,
			},
			expected: true,
		},
		{
			name: "sourceLabels mismatch",
			root: &networking.HTTPMatchRequest{
				SourceLabels: map[string]string{"a": "b"},
			},
			leaf: &networking.HTTPMatchRequest{
				SourceLabels: map[string]string{"a": "c"},
			},
			expected: true,
		},
		{
			name: "sourceNamespace mismatch",
			root: &networking.HTTPMatchRequest{
				SourceNamespace: "test1",
			},
			leaf: &networking.HTTPMatchRequest{
				SourceNamespace: "test2",
			},
			expected: true,
		},
		{
			name: "root has less gateways than delegate",
			root: &networking.HTTPMatchRequest{
				Gateways: []string{"ingress-gateway"},
			},
			leaf: &networking.HTTPMatchRequest{
				Gateways: []string{"ingress-gateway", "mesh"},
			},
			expected: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hasConflict(tc.root, tc.leaf)
			if got != tc.expected {
				t.Errorf("got %v, expected %v", got, tc.expected)
			}
		})
	}
}

// Note: this is to prevent missing merge new added HTTPRoute fields
func TestFuzzMergeHttpRoute(t *testing.T) {
	f := fuzz.New().NilChance(0.5).NumElements(0, 1).Funcs(
		func(r *networking.HTTPRoute, c fuzz.Continue) {
			c.FuzzNoCustom(r)
			r.Match = []*networking.HTTPMatchRequest{{}}
			r.Route = nil
			r.Redirect = nil
			r.Delegate = nil
			r.Mirrors = []*networking.HTTPMirrorPolicy{{}}
		},
		func(r *networking.HTTPMatchRequest, c fuzz.Continue) {
			*r = networking.HTTPMatchRequest{}
		},
		func(r *networking.HTTPRouteDestination, c fuzz.Continue) {
			*r = networking.HTTPRouteDestination{}
		},
		func(r *networking.HTTPRedirect, c fuzz.Continue) {
			*r = networking.HTTPRedirect{}
		},
		func(r *networking.HTTPDirectResponse, c fuzz.Continue) {
			*r = networking.HTTPDirectResponse{}
		},
		func(r *networking.Delegate, c fuzz.Continue) {
			*r = networking.Delegate{}
		},

		func(r *networking.HTTPRewrite, c fuzz.Continue) {
			*r = networking.HTTPRewrite{}
		},

		func(r *durationpb.Duration, c fuzz.Continue) {
			*r = durationpb.Duration{}
		},
		func(r *networking.HTTPRetry, c fuzz.Continue) {
			*r = networking.HTTPRetry{}
		},
		func(r *networking.HTTPFaultInjection, c fuzz.Continue) {
			*r = networking.HTTPFaultInjection{}
		},
		func(r *networking.Destination, c fuzz.Continue) {
			*r = networking.Destination{}
		},
		func(r *wrapperspb.UInt32Value, c fuzz.Continue) {
			*r = wrapperspb.UInt32Value{}
		},
		func(r *networking.Percent, c fuzz.Continue) {
			*r = networking.Percent{}
		},
		func(r *networking.CorsPolicy, c fuzz.Continue) {
			*r = networking.CorsPolicy{}
		},
		func(r *networking.Headers, c fuzz.Continue) {
			*r = networking.Headers{}
		},
		func(r *networking.HTTPMirrorPolicy, c fuzz.Continue) {
			*r = networking.HTTPMirrorPolicy{}
		})

	root := &networking.HTTPRoute{}
	f.Fuzz(root)

	delegate := &networking.HTTPRoute{}
	expected := mergeHTTPRoute(root, delegate)
	assert.Equal(t, expected, root)
}

// Note: this is to prevent missing merge new added HTTPMatchRequest fields
func TestFuzzMergeHttpMatchRequest(t *testing.T) {
	f := fuzz.New().NilChance(0.5).NumElements(1, 1).Funcs(
		func(r *networking.StringMatch, c fuzz.Continue) {
			*r = networking.StringMatch{
				MatchType: &networking.StringMatch_Exact{
					Exact: "fuzz",
				},
			}
		},
		func(m *map[string]*networking.StringMatch, c fuzz.Continue) {
			*m = map[string]*networking.StringMatch{
				"test": nil,
			}
		},
		func(m *map[string]string, c fuzz.Continue) {
			*m = map[string]string{"test": "fuzz"}
		})

	root := &networking.HTTPMatchRequest{}
	f.Fuzz(root)
	root.SourceNamespace = ""
	root.SourceLabels = nil
	root.Gateways = nil
	root.IgnoreUriCase = false
	delegate := &networking.HTTPMatchRequest{}
	merged := mergeHTTPMatchRequest(root, delegate)

	assert.Equal(t, merged, root)
}

var gatewayNameTests = []struct {
	gateway   string
	namespace string
	resolved  string
}{
	{
		"./gateway",
		"default",
		"default/gateway",
	},
	{
		"gateway",
		"default",
		"default/gateway",
	},
	{
		"default/gateway",
		"foo",
		"default/gateway",
	},
	{
		"gateway.default",
		"default",
		"default/gateway",
	},
	{
		"gateway.default",
		"foo",
		"default/gateway",
	},
	{
		"private.ingress.svc.cluster.local",
		"foo",
		"ingress/private",
	},
}

func TestResolveGatewayName(t *testing.T) {
	for _, tt := range gatewayNameTests {
		t.Run(fmt.Sprintf("%s-%s", tt.gateway, tt.namespace), func(t *testing.T) {
			if got := resolveGatewayName(tt.gateway, config.Meta{Namespace: tt.namespace}); got != tt.resolved {
				t.Fatalf("expected %q got %q", tt.resolved, got)
			}
		})
	}
}

func BenchmarkResolveGatewayName(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for _, tt := range gatewayNameTests {
			_ = resolveGatewayName(tt.gateway, config.Meta{Namespace: tt.namespace})
		}
	}
}

func TestSelectVirtualService(t *testing.T) {
	services := []*Service{
		buildHTTPService("bookinfo.com", visibility.Public, wildcardIP, "default", 9999, 70),
		buildHTTPService("private.com", visibility.Private, wildcardIP, "default", 9999, 80),
		buildHTTPService("test.com", visibility.Public, "8.8.8.8", "not-default", 8080),
		buildHTTPService("test-private.com", visibility.Private, "9.9.9.9", "not-default", 80, 70),
		buildHTTPService("test-private-2.com", visibility.Private, "9.9.9.10", "not-default", 60),
		buildHTTPService("test-headless.com", visibility.Public, wildcardIP, "not-default", 8888),
		buildHTTPService("test-headless-someother.com", visibility.Public, wildcardIP, "some-other-ns", 8888),
		buildHTTPService("a.test1.wildcard.com", visibility.Public, wildcardIP, "default", 8888),
		buildHTTPService("*.test2.wildcard.com", visibility.Public, wildcardIP, "default", 8888),
	}

	hostsByNamespace := make(map[string]hostClassification)
	for _, svc := range services {
		ns := svc.Attributes.Namespace
		if _, exists := hostsByNamespace[ns]; !exists {
			hostsByNamespace[ns] = hostClassification{exactHosts: sets.New[host.Name](), allHosts: make([]host.Name, 0)}
		}

		hc := hostsByNamespace[ns]
		hc.allHosts = append(hc.allHosts, svc.Hostname)
		hostsByNamespace[ns] = hc

		if !svc.Hostname.IsWildCarded() {
			hostsByNamespace[ns].exactHosts.Insert(svc.Hostname)
		}
	}

	virtualServiceSpec1 := &networking.VirtualService{
		Hosts:    []string{"test-private-2.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							// Subset: "some-subset",
							Host: "example.org",
							Port: &networking.PortSelector{
								Number: 61,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec2 := &networking.VirtualService{
		Hosts:    []string{"test-private-2.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 62,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec3 := &networking.VirtualService{
		Hosts:    []string{"test-private-3.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 63,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec4 := &networking.VirtualService{
		Hosts:    []string{"test-headless.com", "example.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 64,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec5 := &networking.VirtualService{
		Hosts:    []string{"test-svc.testns.svc.cluster.local"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test-svc.testn.svc.cluster.local",
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec6 := &networking.VirtualService{
		Hosts:    []string{"match-no-service"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "non-exist-service",
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec7 := &networking.VirtualService{
		Hosts:    []string{"test-headless-someother.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 64,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec8 := &networking.VirtualService{
		Hosts:    []string{"*.test1.wildcard.com"}, // match: a.test1.wildcard.com
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 64,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec9 := &networking.VirtualService{
		Hosts:    []string{"foo.test2.wildcard.com"}, // match: *.test2.wildcard.com
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 64,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualService1 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "acme2-v1",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec1,
	}
	virtualService2 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "acme-v2",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec2,
	}
	virtualService3 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec3,
	}
	virtualService4 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "acme-v4",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec4,
	}
	virtualService5 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec5,
	}
	virtualService6 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec6,
	}
	virtualService7 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "acme2-v1",
			Namespace:        "some-other-ns",
		},
		Spec: virtualServiceSpec7,
	}
	virtualService8 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "vs-wildcard-v1",
			Namespace:        "default",
		},
		Spec: virtualServiceSpec8,
	}
	virtualService9 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "service-wildcard-v1",
			Namespace:        "default",
		},
		Spec: virtualServiceSpec9,
	}

	index := virtualServiceIndex{
		publicByGateway: map[string][]config.Config{
			constants.IstioMeshGateway: {
				virtualService1,
				virtualService2,
				virtualService3,
				virtualService4,
				virtualService5,
				virtualService6,
				virtualService7,
				virtualService8,
				virtualService9,
			},
		},
	}

	configs := SelectVirtualServices(index, "some-ns", hostsByNamespace)
	expectedVS := []string{
		virtualService1.Name, virtualService2.Name, virtualService4.Name, virtualService7.Name,
		virtualService8.Name, virtualService9.Name,
	}
	if len(expectedVS) != len(configs) {
		t.Fatalf("Unexpected virtualService, got %d, expected %d", len(configs), len(expectedVS))
	}
	for i, config := range configs {
		if config.Name != expectedVS[i] {
			t.Fatalf("Unexpected virtualService, got %s, expected %s", config.Name, expectedVS[i])
		}
	}
}

func buildHTTPService(hostname string, v visibility.Instance, ip, namespace string, ports ...int) *Service {
	service := &Service{
		CreationTime:   time.Now(),
		Hostname:       host.Name(hostname),
		DefaultAddress: ip,
		Resolution:     DNSLB,
		Attributes: ServiceAttributes{
			ServiceRegistry: provider.Kubernetes,
			Namespace:       namespace,
			ExportTo:        sets.New(v),
		},
	}
	if ip == wildcardIP {
		service.Resolution = Passthrough
	}

	Ports := make([]*Port, 0)

	for _, p := range ports {
		Ports = append(Ports, &Port{
			Name:     fmt.Sprintf("http-%d", p),
			Port:     p,
			Protocol: protocol.HTTP,
		})
	}

	service.Ports = Ports
	return service
}

func BenchmarkSelectVirtualServices(b *testing.B) {
	// Create test data with different sizes to test various scenarios
	benchmarks := []struct {
		name                string
		virtualServiceCount int
		namespaceCount      int
		hostCount           int
		useGatewaySemantics bool
	}{
		{
			name:                "Small-10VS-3NS-5Hosts",
			virtualServiceCount: 10,
			namespaceCount:      3,
			hostCount:           5,
			useGatewaySemantics: false,
		},
		{
			name:                "Medium-100VS-10NS-20Hosts",
			virtualServiceCount: 100,
			namespaceCount:      10,
			hostCount:           20,
			useGatewaySemantics: false,
		},
		{
			name:                "Large-1000VS-20NS-50Hosts",
			virtualServiceCount: 1000,
			namespaceCount:      20,
			hostCount:           50,
			useGatewaySemantics: false,
		},
		{
			name:                "Small-GatewaySemantics-10VS-3NS-5Hosts",
			virtualServiceCount: 10,
			namespaceCount:      3,
			hostCount:           5,
			useGatewaySemantics: true,
		},
		{
			name:                "Medium-GatewaySemantics-100VS-10NS-20Hosts",
			virtualServiceCount: 100,
			namespaceCount:      10,
			hostCount:           20,
			useGatewaySemantics: true,
		},
		{
			name:                "Large-GatewaySemantics-1000VS-20NS-50Hosts",
			virtualServiceCount: 1000,
			namespaceCount:      20,
			hostCount:           50,
			useGatewaySemantics: true,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Create test data
			vsidx, hostsByNamespace := createBenchmarkData(bm.virtualServiceCount, bm.namespaceCount, bm.hostCount, bm.useGatewaySemantics)
			configNamespace := "default"

			// Reset timer before the actual benchmark
			b.ResetTimer()

			// Run the benchmark
			for i := 0; i < b.N; i++ {
				_ = SelectVirtualServices(vsidx, configNamespace, hostsByNamespace)
			}
		})
	}
}

// createBenchmarkData creates test data for benchmarking SelectVirtualServices
func createBenchmarkData(vsCount, nsCount, hostCount int, useGatewaySemantics bool) (virtualServiceIndex, map[string]hostClassification) {
	vsidx := virtualServiceIndex{
		publicByGateway:              make(map[string][]config.Config),
		privateByNamespaceAndGateway: make(map[types.NamespacedName][]config.Config),
		exportedToNamespaceByGateway: make(map[types.NamespacedName][]config.Config),
		delegates:                    make(map[ConfigKey][]ConfigKey),
		destinationsByGateway:        make(map[string]sets.String),
		referencedDestinations:       make(map[string]sets.String),
	}

	hostsByNamespace := make(map[string]hostClassification)

	// Create namespaces
	namespaces := make([]string, nsCount)
	for i := 0; i < nsCount; i++ {
		namespaces[i] = fmt.Sprintf("namespace-%d", i)
		hostsByNamespace[namespaces[i]] = hostClassification{
			exactHosts: sets.New[host.Name](),
			allHosts:   make([]host.Name, 0, hostCount),
		}
	}

	// Add wildcard namespace
	hostsByNamespace[wildcardNamespace] = hostClassification{
		exactHosts: sets.New[host.Name](),
		allHosts:   make([]host.Name, 0, hostCount),
	}

	// Create hosts for each namespace
	for _, ns := range namespaces {
		hc := hostsByNamespace[ns]
		for i := 0; i < hostCount; i++ {
			hostname := host.Name(fmt.Sprintf("host-%d.%s.com", i, ns))
			hc.allHosts = append(hc.allHosts, hostname)
			if !hostname.IsWildCarded() {
				hc.exactHosts.Insert(hostname)
			}
		}
		hostsByNamespace[ns] = hc

		// Add some hosts to wildcard namespace
		wcHc := hostsByNamespace[wildcardNamespace]
		for i := 0; i < hostCount/2; i++ {
			hostname := host.Name(fmt.Sprintf("wildcard-host-%d.com", i))
			wcHc.allHosts = append(wcHc.allHosts, hostname)
			if !hostname.IsWildCarded() {
				wcHc.exactHosts.Insert(hostname)
			}
		}
		hostsByNamespace[wildcardNamespace] = wcHc
	}

	// Create virtual services
	vsidx.publicByGateway[constants.IstioMeshGateway] = make([]config.Config, 0, vsCount/2)
	vsidx.privateByNamespaceAndGateway[types.NamespacedName{Namespace: "default", Name: constants.IstioMeshGateway}] = make([]config.Config, 0, vsCount/4)
	vsidx.exportedToNamespaceByGateway[types.NamespacedName{Namespace: "default", Name: constants.IstioMeshGateway}] = make([]config.Config, 0, vsCount/4)

	for i := 0; i < vsCount; i++ {
		// Distribute virtual services across different categories
		vs := createVirtualService(i, namespaces[i%len(namespaces)], useGatewaySemantics)

		if i < vsCount/2 {
			// Public virtual services
			vsidx.publicByGateway[constants.IstioMeshGateway] = append(vsidx.publicByGateway[constants.IstioMeshGateway], vs)
		} else if i < 3*vsCount/4 {
			// Private virtual services
			vsidx.privateByNamespaceAndGateway[types.NamespacedName{Namespace: "default", Name: constants.IstioMeshGateway}] = append(
				vsidx.privateByNamespaceAndGateway[types.NamespacedName{Namespace: "default", Name: constants.IstioMeshGateway}], vs)
		} else {
			// Exported virtual services
			vsidx.exportedToNamespaceByGateway[types.NamespacedName{Namespace: "default", Name: constants.IstioMeshGateway}] = append(
				vsidx.exportedToNamespaceByGateway[types.NamespacedName{Namespace: "default", Name: constants.IstioMeshGateway}], vs)
		}
	}

	return vsidx, hostsByNamespace
}

// createVirtualService creates a virtual service for benchmarking
func createVirtualService(index int, namespace string, useGatewaySemantics bool) config.Config {
	// Create hosts that will match some of the test hosts
	hosts := []string{
		fmt.Sprintf("host-%d.%s.com", index%5, namespace),
		fmt.Sprintf("wildcard-host-%d.com", index%3),
	}

	// Add some wildcard hosts
	if index%7 == 0 {
		hosts = append(hosts, fmt.Sprintf("*.%s.com", namespace))
	}

	vs := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             fmt.Sprintf("vs-%d", index),
			Namespace:        namespace,
		},
		Spec: &networking.VirtualService{
			Hosts:    hosts,
			Gateways: []string{constants.IstioMeshGateway},
			Http: []*networking.HTTPRoute{
				{
					Route: []*networking.HTTPRouteDestination{
						{
							Destination: &networking.Destination{
								Host: fmt.Sprintf("destination-%d.com", index),
								Port: &networking.PortSelector{
									Number: uint32(80 + index%20),
								},
							},
							Weight: int32(100),
						},
					},
				},
			},
		},
	}

	// If using gateway semantics, add some additional configuration
	if useGatewaySemantics {
		// Add some match conditions to make it more realistic
		vs.Spec.(*networking.VirtualService).Http[0].Match = []*networking.HTTPMatchRequest{
			{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{
						Prefix: fmt.Sprintf("/api/v%d", index%5),
					},
				},
			},
		}
	}

	return vs
}
