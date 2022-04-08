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

	"github.com/golang/protobuf/ptypes/wrappers"
	fuzz "github.com/google/gofuzz"
	"google.golang.org/protobuf/types/known/durationpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/test/util/assert"
)

const wildcardIP = "0.0.0.0"

func TestMergeVirtualServices(t *testing.T) {
	independentVs := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
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
	}{
		{
			name:                    "one independent vs",
			virtualServices:         []config.Config{independentVs},
			expectedVirtualServices: []config.Config{independentVs},
		},
		{
			name:                    "one root vs",
			virtualServices:         []config.Config{rootVs},
			expectedVirtualServices: []config.Config{oneRoot},
		},
		{
			name:                    "one delegate vs",
			virtualServices:         []config.Config{delegateVs},
			expectedVirtualServices: []config.Config{},
		},
		{
			name:                    "root and delegate vs",
			virtualServices:         []config.Config{rootVs.DeepCopy(), delegateVs},
			expectedVirtualServices: []config.Config{mergedVs},
		},
		{
			name:                    "root and conflicted delegate vs",
			virtualServices:         []config.Config{rootVs.DeepCopy(), delegateVs2},
			expectedVirtualServices: []config.Config{mergedVs2},
		},
		{
			name:                    "multiple routes delegate to one",
			virtualServices:         []config.Config{multiRoutes.DeepCopy(), singleDelegate},
			expectedVirtualServices: []config.Config{mergedVs3},
		},
		{
			name:                    "root not specify delegate namespace",
			virtualServices:         []config.Config{defaultVs.DeepCopy(), delegateVsExportedToAll},
			expectedVirtualServices: []config.Config{mergedVsInDefault},
		},
		{
			name:                    "delegate not exported to root vs namespace",
			virtualServices:         []config.Config{rootVs, delegateVsNotExported},
			expectedVirtualServices: []config.Config{oneRoot},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := mergeVirtualServicesIfNeeded(tc.virtualServices, map[visibility.Instance]bool{visibility.Public: true})
			assert.Equal(t, got, tc.expectedVirtualServices)
		})
	}
}

func TestMergeHttpRoutes(t *testing.T) {
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
					// default route to v3
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
							Port: 8080,
						},
					},
					// default route to v3
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
		func(r *wrappers.UInt32Value, c fuzz.Continue) {
			*r = wrappers.UInt32Value{}
		},
		func(r *networking.Percent, c fuzz.Continue) {
			*r = networking.Percent{}
		},
		func(r *networking.CorsPolicy, c fuzz.Continue) {
			*r = networking.CorsPolicy{}
		},
		func(r *networking.Headers, c fuzz.Continue) {
			*r = networking.Headers{}
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
	}

	hostsByNamespace := make(map[string][]host.Name)
	for _, svc := range services {
		hostsByNamespace[svc.Attributes.Namespace] = append(hostsByNamespace[svc.Attributes.Namespace], svc.Hostname)
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
	virtualService1 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme2-v1",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec1,
	}
	virtualService2 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v2",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec2,
	}
	virtualService3 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec3,
	}
	virtualService4 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v4",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec4,
	}
	virtualService5 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec5,
	}
	virtualService6 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec6,
	}
	virtualService7 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme2-v1",
			Namespace:        "some-other-ns",
		},
		Spec: virtualServiceSpec7,
	}
	configs := SelectVirtualServices(
		[]config.Config{virtualService1, virtualService2, virtualService3, virtualService4, virtualService5, virtualService6, virtualService7},
		hostsByNamespace)
	expectedVS := []string{virtualService1.Name, virtualService2.Name, virtualService4.Name, virtualService7.Name}
	if len(expectedVS) != len(configs) {
		t.Fatalf("Unexpected virtualService, got %d, epxected %d", len(configs), len(expectedVS))
	}
	for i, config := range configs {
		if config.Name != expectedVS[i] {
			t.Fatalf("Unexpected virtualService, got %s, epxected %s", config.Name, expectedVS[i])
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
			ExportTo:        map[visibility.Instance]bool{v: true},
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
