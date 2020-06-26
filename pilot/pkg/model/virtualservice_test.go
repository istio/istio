// Copyright 2020 Istio Authors
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

	"github.com/gogo/protobuf/types"
	"github.com/google/go-cmp/cmp"
	fuzz "github.com/google/gofuzz"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestMergeVirtualServices(t *testing.T) {
	features.EnableVirtualServiceDelegate = true
	independentVs := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
			Name:      "virtual-service",
			Namespace: "default",
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

	rootVs := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
			Name:      "root-vs",
			Namespace: "default",
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

	oneRoot := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
			Name:      "root-vs",
			Namespace: "default",
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

	delegateVs := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
			Name:      "productpage-vs",
			Namespace: "default",
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

	mergedVs := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
			Name:      "root-vs",
			Namespace: "default",
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

	// invalid delegate, match condition conflicts with root
	delegateVs2 := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
			Name:      "productpage-vs",
			Namespace: "default",
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
					// mis match, this route will be ignored
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

	mergedVs2 := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
			Name:      "root-vs",
			Namespace: "default",
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
	multiRoutes := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
			Name:      "root-vs",
			Namespace: "default",
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

	singleDelegate := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
			Name:      "productpage-vs",
			Namespace: "default",
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

	mergedVs3 := Config{
		ConfigMeta: ConfigMeta{
			Type:      collections.IstioNetworkingV1Alpha3Virtualservices.Resource().Kind(),
			Name:      "root-vs",
			Namespace: "default",
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
		virtualServices         []Config
		expectedVirtualServices []Config
	}{
		{
			name:                    "one independent vs",
			virtualServices:         []Config{independentVs},
			expectedVirtualServices: []Config{independentVs},
		},
		{
			name:                    "one root vs",
			virtualServices:         []Config{rootVs},
			expectedVirtualServices: []Config{oneRoot},
		},
		{
			name:                    "one delegate vs",
			virtualServices:         []Config{delegateVs},
			expectedVirtualServices: []Config{},
		},
		{
			name:                    "root and delegate vs",
			virtualServices:         []Config{rootVs.DeepCopy(), delegateVs},
			expectedVirtualServices: []Config{mergedVs},
		},
		{
			name:                    "root and conflicted delegate vs",
			virtualServices:         []Config{rootVs.DeepCopy(), delegateVs2},
			expectedVirtualServices: []Config{mergedVs2},
		},
		{
			name:                    "multiple routes delegate to one",
			virtualServices:         []Config{multiRoutes.DeepCopy(), singleDelegate},
			expectedVirtualServices: []Config{mergedVs3},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeVirtualServicesIfNeeded(tc.virtualServices)
			if !reflect.DeepEqual(got, tc.expectedVirtualServices) {
				t.Errorf("expected vs %v, but got %v,\n diff: %s ", len(tc.expectedVirtualServices), len(got), cmp.Diff(tc.expectedVirtualServices, got))
			}
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
				Timeout: &types.Duration{Seconds: 10},
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
					Timeout: &types.Duration{Seconds: 10},
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
					Timeout: &types.Duration{Seconds: 10},
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
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got unexpected result, diff: %s", cmp.Diff(tc.expected, got))
			}
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
					}},
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
					Port: 9090, // conflicts
				},
			},
			expected: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := mergeHTTPMatchRequests(tc.root, tc.delegate)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got unexpected result, diff: %s", cmp.Diff(tc.expected, got))
			}
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

		func(r *types.Duration, c fuzz.Continue) {
			*r = types.Duration{}
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
		func(r *types.UInt32Value, c fuzz.Continue) {
			*r = types.UInt32Value{}
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
	root.XXX_unrecognized = nil
	root.XXX_sizecache = 0
	if !reflect.DeepEqual(expected, root) {
		t.Errorf("%s", cmp.Diff(expected, root))
	}
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
	root.XXX_sizecache = 0
	root.XXX_unrecognized = nil

	delegate := &networking.HTTPMatchRequest{}
	merged := mergeHTTPMatchRequest(root, delegate)

	if !reflect.DeepEqual(merged, root) {
		t.Errorf("%s", cmp.Diff(merged, root))
	}
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
}

func TestResolveGatewayName(t *testing.T) {
	for _, tt := range gatewayNameTests {
		t.Run(fmt.Sprintf("%s-%s", tt.gateway, tt.namespace), func(t *testing.T) {
			if got := resolveGatewayName(tt.gateway, ConfigMeta{Namespace: tt.namespace}); got != tt.resolved {
				t.Fatalf("expected %q got %q", tt.resolved, got)
			}
		})
	}
}

func BenchmarkResolveGatewayName(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for _, tt := range gatewayNameTests {
			_ = resolveGatewayName(tt.gateway, ConfigMeta{Namespace: tt.namespace})
		}
	}
}
