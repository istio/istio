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

package virtualservice

import (
	"testing"

	"istio.io/api/networking/v1alpha3"
)

func TestGetRouteDestinations(t *testing.T) {
	tests := []struct {
		name     string
		vs       *v1alpha3.VirtualService
		expected int
	}{
		{
			name:     "empty virtual service",
			vs:       &v1alpha3.VirtualService{},
			expected: 0,
		},
		{
			name: "single http route",
			vs: &v1alpha3.VirtualService{
				Http: []*v1alpha3.HTTPRoute{
					{
						Route: []*v1alpha3.HTTPRouteDestination{
							{Destination: &v1alpha3.Destination{Host: "reviews"}},
						},
					},
				},
			},
			expected: 1,
		},
		{
			name: "multiple http routes with multiple destinations",
			vs: &v1alpha3.VirtualService{
				Http: []*v1alpha3.HTTPRoute{
					{
						Route: []*v1alpha3.HTTPRouteDestination{
							{Destination: &v1alpha3.Destination{Host: "reviews"}},
							{Destination: &v1alpha3.Destination{Host: "ratings"}},
						},
					},
					{
						Route: []*v1alpha3.HTTPRouteDestination{
							{Destination: &v1alpha3.Destination{Host: "details"}},
						},
					},
				},
			},
			expected: 3,
		},
		{
			name: "tcp routes",
			vs: &v1alpha3.VirtualService{
				Tcp: []*v1alpha3.TCPRoute{
					{
						Route: []*v1alpha3.RouteDestination{
							{Destination: &v1alpha3.Destination{Host: "mongo"}},
						},
					},
				},
			},
			expected: 1,
		},
		{
			name: "tls routes",
			vs: &v1alpha3.VirtualService{
				Tls: []*v1alpha3.TLSRoute{
					{
						Route: []*v1alpha3.RouteDestination{
							{Destination: &v1alpha3.Destination{Host: "secure-service"}},
							{Destination: &v1alpha3.Destination{Host: "secure-service-v2"}},
						},
					},
				},
			},
			expected: 2,
		},
		{
			name: "mixed routes",
			vs: &v1alpha3.VirtualService{
				Http: []*v1alpha3.HTTPRoute{
					{
						Route: []*v1alpha3.HTTPRouteDestination{
							{Destination: &v1alpha3.Destination{Host: "reviews"}},
						},
					},
				},
				Tcp: []*v1alpha3.TCPRoute{
					{
						Route: []*v1alpha3.RouteDestination{
							{Destination: &v1alpha3.Destination{Host: "mongo"}},
						},
					},
				},
				Tls: []*v1alpha3.TLSRoute{
					{
						Route: []*v1alpha3.RouteDestination{
							{Destination: &v1alpha3.Destination{Host: "secure-service"}},
						},
					},
				},
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destinations := getRouteDestinations(tt.vs)
			if len(destinations) != tt.expected {
				t.Errorf("getRouteDestinations() returned %d destinations, expected %d", len(destinations), tt.expected)
			}
		})
	}
}

func TestGetRouteDestinationsAnnotations(t *testing.T) {
	vs := &v1alpha3.VirtualService{
		Http: []*v1alpha3.HTTPRoute{
			{
				Route: []*v1alpha3.HTTPRouteDestination{
					{Destination: &v1alpha3.Destination{Host: "reviews"}},
				},
			},
		},
		Tcp: []*v1alpha3.TCPRoute{
			{
				Route: []*v1alpha3.RouteDestination{
					{Destination: &v1alpha3.Destination{Host: "mongo"}},
				},
			},
		},
		Tls: []*v1alpha3.TLSRoute{
			{
				Route: []*v1alpha3.RouteDestination{
					{Destination: &v1alpha3.Destination{Host: "secure-service"}},
				},
			},
		},
	}

	destinations := getRouteDestinations(vs)

	// Check that annotations are correctly set
	routeRules := map[string]bool{}
	for _, d := range destinations {
		routeRules[d.RouteRule] = true
	}

	if !routeRules["http"] {
		t.Error("expected http route rule in destinations")
	}
	if !routeRules["tcp"] {
		t.Error("expected tcp route rule in destinations")
	}
	if !routeRules["tls"] {
		t.Error("expected tls route rule in destinations")
	}
}

func TestGetHTTPMirrorDestinations(t *testing.T) {
	tests := []struct {
		name     string
		vs       *v1alpha3.VirtualService
		expected int
	}{
		{
			name:     "empty virtual service",
			vs:       &v1alpha3.VirtualService{},
			expected: 0,
		},
		{
			name: "http route without mirror",
			vs: &v1alpha3.VirtualService{
				Http: []*v1alpha3.HTTPRoute{
					{
						Route: []*v1alpha3.HTTPRouteDestination{
							{Destination: &v1alpha3.Destination{Host: "reviews"}},
						},
					},
				},
			},
			expected: 0,
		},
		{
			name: "http route with single mirror",
			vs: &v1alpha3.VirtualService{
				Http: []*v1alpha3.HTTPRoute{
					{
						Route: []*v1alpha3.HTTPRouteDestination{
							{Destination: &v1alpha3.Destination{Host: "reviews"}},
						},
						Mirror: &v1alpha3.Destination{Host: "reviews-v2"},
					},
				},
			},
			expected: 1,
		},
		{
			name: "http route with multiple mirrors",
			vs: &v1alpha3.VirtualService{
				Http: []*v1alpha3.HTTPRoute{
					{
						Route: []*v1alpha3.HTTPRouteDestination{
							{Destination: &v1alpha3.Destination{Host: "reviews"}},
						},
						Mirrors: []*v1alpha3.HTTPMirrorPolicy{
							{Destination: &v1alpha3.Destination{Host: "reviews-v2"}},
							{Destination: &v1alpha3.Destination{Host: "reviews-v3"}},
						},
					},
				},
			},
			expected: 2,
		},
		{
			name: "http route with both mirror and mirrors",
			vs: &v1alpha3.VirtualService{
				Http: []*v1alpha3.HTTPRoute{
					{
						Route: []*v1alpha3.HTTPRouteDestination{
							{Destination: &v1alpha3.Destination{Host: "reviews"}},
						},
						Mirror: &v1alpha3.Destination{Host: "reviews-mirror"},
						Mirrors: []*v1alpha3.HTTPMirrorPolicy{
							{Destination: &v1alpha3.Destination{Host: "reviews-v2"}},
						},
					},
				},
			},
			expected: 2,
		},
		{
			name: "multiple http routes with mirrors",
			vs: &v1alpha3.VirtualService{
				Http: []*v1alpha3.HTTPRoute{
					{
						Mirror: &v1alpha3.Destination{Host: "reviews-mirror-1"},
					},
					{
						Mirror: &v1alpha3.Destination{Host: "reviews-mirror-2"},
					},
				},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destinations := getHTTPMirrorDestinations(tt.vs)
			if len(destinations) != tt.expected {
				t.Errorf("getHTTPMirrorDestinations() returned %d destinations, expected %d", len(destinations), tt.expected)
			}
		})
	}
}

func TestGetHTTPMirrorDestinationsAnnotations(t *testing.T) {
	vs := &v1alpha3.VirtualService{
		Http: []*v1alpha3.HTTPRoute{
			{
				Mirror: &v1alpha3.Destination{Host: "reviews-mirror"},
				Mirrors: []*v1alpha3.HTTPMirrorPolicy{
					{Destination: &v1alpha3.Destination{Host: "reviews-v2"}},
				},
			},
		},
	}

	destinations := getHTTPMirrorDestinations(vs)

	// Check that annotations are correctly set
	routeRules := map[string]bool{}
	for _, d := range destinations {
		routeRules[d.RouteRule] = true
	}

	if !routeRules["http.mirror"] {
		t.Error("expected http.mirror route rule in destinations")
	}
	if !routeRules["http.mirrors"] {
		t.Error("expected http.mirrors route rule in destinations")
	}
}
