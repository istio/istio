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
	"os"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/google/go-cmp/cmp"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestMergeVirtualServices(t *testing.T) {
	os.Setenv("PILOT_ENABLE_VIRTUAL_SERVICE_DELEGATE", "true")
	defer os.Unsetenv("PILOT_ENABLE_VIRTUAL_SERVICE_DELEGATE")
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

func TestMergeHttpRoute(t *testing.T) {
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
			got := mergeHTTPRoute(tc.root, tc.delegate)
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
			name: "source labels/namespaces",
			root: []*networking.HTTPMatchRequest{
				{
					SourceNamespace: "default",
					SourceLabels:    map[string]string{"version": "v1"},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					SourceLabels: map[string]string{"app": "productpage"},
				},
			},
			expected: []*networking.HTTPMatchRequest{
				{
					SourceLabels:    map[string]string{"app": "productpage", "version": "v1"},
					SourceNamespace: "default",
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
					SourceLabels: map[string]string{"app": "productpage"},
					Port:         8080,
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
					SourceLabels: map[string]string{"version": "v1"},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v2"},
					},
					SourceLabels: map[string]string{"version": "v2"},
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
					SourceLabels: map[string]string{"app": "productpage", "version": "v1"},
					Port:         8080,
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
					SourceLabels: map[string]string{"app": "productpage", "version": "v2"},
					Port:         8080,
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
					SourceLabels: map[string]string{"app": "productpage"},
					Port:         8080,
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
					SourceLabels: map[string]string{"version": "v1"},
				},
				{
					Uri: &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: "/productpage/v2"},
					},
					SourceLabels: map[string]string{"version": "v2"},
					Port:         9090, // conflicts
				},
			},
			expected: nil,
		},
		{
			name: "gateways",
			root: []*networking.HTTPMatchRequest{
				{
					Gateways: nil,
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Gateways: []string{"mesh", "ingress-gateway"},
				},
			},
			expected: []*networking.HTTPMatchRequest{
				{
					Gateways: []string{"mesh", "ingress-gateway"},
				},
			},
		},
		{
			name: "gateways conflict",
			root: []*networking.HTTPMatchRequest{
				{
					Gateways: []string{"mesh"},
				},
			},
			delegate: []*networking.HTTPMatchRequest{
				{
					Gateways: []string{"mesh", "ingress-gateway"},
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
			name: "source labels",
			root: &networking.HTTPMatchRequest{
				SourceLabels: nil,
			},
			leaf: &networking.HTTPMatchRequest{
				SourceLabels: map[string]string{"app": "product"},
			},
			expected: false,
		},
		{
			name: "source labels",
			root: &networking.HTTPMatchRequest{
				SourceLabels: map[string]string{"app": "product"},
			},
			leaf:     &networking.HTTPMatchRequest{},
			expected: false,
		},
		{
			name: "source labels",
			root: &networking.HTTPMatchRequest{
				SourceLabels: map[string]string{"version": "v1"},
			},
			leaf: &networking.HTTPMatchRequest{
				SourceLabels: map[string]string{"app": "product"},
			},
			expected: false,
		},
		{
			name: "source labels",
			root: &networking.HTTPMatchRequest{
				SourceLabels: map[string]string{"version": "v1"},
			},
			leaf: &networking.HTTPMatchRequest{
				SourceLabels: map[string]string{"version": "v2"},
			},
			expected: true,
		},
		{
			name: "source namespace",
			root: &networking.HTTPMatchRequest{
				SourceNamespace: "default",
			},
			leaf: &networking.HTTPMatchRequest{
				SourceNamespace: "",
			},
			expected: false,
		},
		{
			name: "source namespace",
			root: &networking.HTTPMatchRequest{
				SourceNamespace: "",
			},
			leaf: &networking.HTTPMatchRequest{
				SourceNamespace: "default",
			},
			expected: false,
		},
		{
			name: "source namespace",
			root: &networking.HTTPMatchRequest{
				SourceNamespace: "default",
			},
			leaf: &networking.HTTPMatchRequest{
				SourceNamespace: "default",
			},
			expected: false,
		},
		{
			name: "diff source namespace",
			root: &networking.HTTPMatchRequest{
				SourceNamespace: "default",
			},
			leaf: &networking.HTTPMatchRequest{
				SourceNamespace: "istio",
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
			name: "gateway",
			root: &networking.HTTPMatchRequest{
				Gateways: nil,
			},
			leaf: &networking.HTTPMatchRequest{
				Gateways: []string{"mesh"},
			},
			expected: false,
		},
		{
			name: "gateway",
			root: &networking.HTTPMatchRequest{
				Gateways: []string{"mesh", "ingress-gateway"},
			},
			leaf: &networking.HTTPMatchRequest{
				Gateways: []string{"mesh"},
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
