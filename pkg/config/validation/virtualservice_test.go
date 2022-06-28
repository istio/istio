// Copyright 2010 Istio Authors
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

package validation

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
)

func TestValidateChainingVirtualService(t *testing.T) {
	testCases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{
			name: "root simple",
			in: &networking.VirtualService{
				Hosts:    []string{"foo.bar"},
				Gateways: []string{"test-gateway"},
				Http: []*networking.HTTPRoute{{
					Delegate: &networking.Delegate{
						Name:      "test",
						Namespace: "test",
					},
				}},
			},
			valid: true,
		},
		{
			name: "root applies to sidecar with delegate",
			in: &networking.VirtualService{
				Hosts: []string{"foo.bar"},
				Http: []*networking.HTTPRoute{{
					Delegate: &networking.Delegate{
						Name:      "test",
						Namespace: "test",
					},
				}},
			},
			valid: true,
		},
		{
			name: "root with delegate and destination in one route",
			in: &networking.VirtualService{
				Hosts: []string{"foo.bar"},
				Http: []*networking.HTTPRoute{{
					Delegate: &networking.Delegate{
						Name:      "test",
						Namespace: "test",
					},
					Route: []*networking.HTTPRouteDestination{{
						Destination: &networking.Destination{Host: "foo.baz"},
					}},
				}},
			},
			valid: false,
		},
		{
			name: "delegate with no destination",
			in: &networking.VirtualService{
				Hosts: []string{},
				Http:  []*networking.HTTPRoute{{}},
			},
			valid: false,
		},
		{
			name: "delegate with delegate",
			in: &networking.VirtualService{
				Hosts: []string{},
				Http: []*networking.HTTPRoute{
					{
						Delegate: &networking.Delegate{
							Name:      "test",
							Namespace: "test",
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "delegate with TCP route",
			in: &networking.VirtualService{
				Hosts: []string{},
				Tcp: []*networking.TCPRoute{{
					Route: []*networking.RouteDestination{{
						Destination: &networking.Destination{Host: "foo.baz"},
					}},
				}},
			},
			valid: false,
		},
		{
			name: "delegate with gateway",
			in: &networking.VirtualService{
				Hosts:    []string{},
				Gateways: []string{"test"},
				Http: []*networking.HTTPRoute{{
					Route: []*networking.HTTPRouteDestination{{
						Destination: &networking.Destination{Host: "foo.baz"},
					}},
				}},
			},
			valid: false,
		},
		{
			name: "delegate with sourceNamespace",
			in: &networking.VirtualService{
				Hosts: []string{},
				Http: []*networking.HTTPRoute{{
					Match: []*networking.HTTPMatchRequest{
						{
							SourceNamespace: "test",
						},
					},
					Route: []*networking.HTTPRouteDestination{{
						Destination: &networking.Destination{Host: "foo.baz"},
					}},
				}},
			},
			valid: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateVirtualService(config.Config{Spec: tc.in}); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateRootHTTPRoute(t *testing.T) {
	testCases := []struct {
		name  string
		route *networking.HTTPRoute
		valid bool
	}{
		{
			name: "simple",
			route: &networking.HTTPRoute{
				Delegate: &networking.Delegate{
					Name:      "test",
					Namespace: "test",
				},
			},
			valid: true,
		},
		{
			name: "route and delegate conflict",
			route: &networking.HTTPRoute{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
				Delegate: &networking.Delegate{
					Name:      "test",
					Namespace: "test",
				},
			},
			valid: false,
		},
		{
			name: "redirect and delegate conflict",
			route: &networking.HTTPRoute{
				Redirect: &networking.HTTPRedirect{
					Uri:       "/lerp",
					Authority: "foo.biz",
				},
				Delegate: &networking.Delegate{
					Name:      "test",
					Namespace: "test",
				},
			},
			valid: false,
		},
		{
			name: "null header match", route: &networking.HTTPRoute{
				Delegate: &networking.Delegate{
					Name:      "test",
					Namespace: "test",
				},
				Match: []*networking.HTTPMatchRequest{{
					Headers: map[string]*networking.StringMatch{
						"header": nil,
					},
				}},
			}, valid: false,
		},
		{
			name: "prefix header match", route: &networking.HTTPRoute{
				Delegate: &networking.Delegate{
					Name:      "test",
					Namespace: "test",
				},
				Match: []*networking.HTTPMatchRequest{{
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Prefix{Prefix: "test"},
						},
					},
				}},
			}, valid: true,
		},
		{
			name: "exact header match", route: &networking.HTTPRoute{
				Delegate: &networking.Delegate{
					Name:      "test",
					Namespace: "test",
				},
				Match: []*networking.HTTPMatchRequest{{
					Headers: map[string]*networking.StringMatch{
						"header": {
							MatchType: &networking.StringMatch_Exact{Exact: "test"},
						},
					},
				}},
			}, valid: true,
		},
		{name: "regex header match", route: &networking.HTTPRoute{
			Delegate: &networking.Delegate{
				Name:      "test",
				Namespace: "test",
			},
			Match: []*networking.HTTPMatchRequest{{
				Headers: map[string]*networking.StringMatch{
					"header": {
						MatchType: &networking.StringMatch_Regex{Regex: "test"},
					},
				},
			}},
		}, valid: true},
		{name: "regex uri match", route: &networking.HTTPRoute{
			Delegate: &networking.Delegate{
				Name:      "test",
				Namespace: "test",
			},
			Match: []*networking.HTTPMatchRequest{{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{Regex: "test"},
				},
			}},
		}, valid: true},
		{name: "prefix uri match", route: &networking.HTTPRoute{
			Delegate: &networking.Delegate{
				Name:      "test",
				Namespace: "test",
			},
			Match: []*networking.HTTPMatchRequest{{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: "test"},
				},
			}},
		}, valid: true},
		{name: "exact uri match", route: &networking.HTTPRoute{
			Delegate: &networking.Delegate{
				Name:      "test",
				Namespace: "test",
			},
			Match: []*networking.HTTPMatchRequest{{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Exact{Exact: "test"},
				},
			}},
		}, valid: true},
		{
			name: "prefix queryParams match", route: &networking.HTTPRoute{
				Delegate: &networking.Delegate{
					Name:      "test",
					Namespace: "test",
				},
				Match: []*networking.HTTPMatchRequest{{
					QueryParams: map[string]*networking.StringMatch{
						"q": {
							MatchType: &networking.StringMatch_Prefix{Prefix: "test"},
						},
					},
				}},
			}, valid: true,
		},
		{
			name: "exact queryParams match", route: &networking.HTTPRoute{
				Delegate: &networking.Delegate{
					Name:      "test",
					Namespace: "test",
				},
				Match: []*networking.HTTPMatchRequest{{
					QueryParams: map[string]*networking.StringMatch{
						"q": {
							MatchType: &networking.StringMatch_Exact{Exact: "test"},
						},
					},
				}},
			}, valid: true,
		},
		{name: "regex queryParams match", route: &networking.HTTPRoute{
			Delegate: &networking.Delegate{
				Name:      "test",
				Namespace: "test",
			},
			Match: []*networking.HTTPMatchRequest{{
				Headers: map[string]*networking.StringMatch{
					"q": {
						MatchType: &networking.StringMatch_Regex{Regex: "test"},
					},
				},
			}},
		}, valid: true},
		{name: "empty regex match in method", route: &networking.HTTPRoute{
			Match: []*networking.HTTPMatchRequest{{
				Method: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{Regex: ""},
				},
			}},
			Redirect: &networking.HTTPRedirect{
				Uri:       "/",
				Authority: "foo.biz",
			},
		}, valid: false},
		{name: "empty regex match in uri", route: &networking.HTTPRoute{
			Match: []*networking.HTTPMatchRequest{{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{Regex: ""},
				},
			}},
			Redirect: &networking.HTTPRedirect{
				Uri:       "/",
				Authority: "foo.biz",
			},
		}, valid: false},
		{name: "empty regex match in query", route: &networking.HTTPRoute{
			Match: []*networking.HTTPMatchRequest{{
				QueryParams: map[string]*networking.StringMatch{
					"q": {
						MatchType: &networking.StringMatch_Regex{Regex: ""},
					},
				},
			}},
			Redirect: &networking.HTTPRedirect{
				Uri:       "/",
				Authority: "foo.biz",
			},
		}, valid: false},
		{name: "empty regex match in scheme", route: &networking.HTTPRoute{
			Match: []*networking.HTTPMatchRequest{{
				Scheme: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{Regex: ""},
				},
			}},
			Redirect: &networking.HTTPRedirect{
				Uri:       "/",
				Authority: "foo.biz",
			},
		}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateHTTPRoute(tc.route, false); (err.Err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err.Err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateDelegateHTTPRoute(t *testing.T) {
	testCases := []struct {
		name  string
		route *networking.HTTPRoute
		valid bool
	}{
		{name: "empty", route: &networking.HTTPRoute{ // nothing
		}, valid:                                     false},
		{name: "simple", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
		}, valid: true},
		{name: "no destination", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: nil,
			}},
		}, valid: false},
		{name: "weighted", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      25,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      75,
			}},
		}, valid: true},
		{name: "total weight > 100", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      55,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      50,
			}},
		}, valid: true},
		{name: "total weight < 100", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      49,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      50,
			}},
		}, valid: true},
		{name: "simple redirect", route: &networking.HTTPRoute{
			Redirect: &networking.HTTPRedirect{
				Uri:       "/lerp",
				Authority: "foo.biz",
			},
		}, valid: true},
		{name: "conflicting redirect and route", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
			Redirect: &networking.HTTPRedirect{
				Uri:       "/lerp",
				Authority: "foo.biz",
			},
		}, valid: false},
		{name: "request response headers", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
		}, valid: true},
		{name: "valid headers", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"name": "",
						},
						Set: map[string]string{
							"name": "",
						},
						Remove: []string{
							"name",
						},
					},
					Response: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"name": "",
						},
						Set: map[string]string{
							"name": "",
						},
						Remove: []string{
							"name",
						},
					},
				},
			}},
		}, valid: true},
		{name: "empty header name - request add", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - request set", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Set: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - request remove", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Remove: []string{
							"",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - response add", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - response set", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Set: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - response remove", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Remove: []string{
							"",
						},
					},
				},
			}},
		}, valid: false},
		{name: "null header match", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{{
				Headers: map[string]*networking.StringMatch{
					"header": nil,
				},
			}},
		}, valid: false},
		{name: "nil match", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: nil,
		}, valid: true},
		{name: "match with nil element", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: true},
		{name: "invalid mirror percent", route: &networking.HTTPRoute{
			MirrorPercent: &wrapperspb.UInt32Value{Value: 101},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "invalid mirror percentage", route: &networking.HTTPRoute{
			MirrorPercentage: &networking.Percent{
				Value: 101,
			},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "valid mirror percentage", route: &networking.HTTPRoute{
			MirrorPercentage: &networking.Percent{
				Value: 1,
			},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: true},
		{name: "negative mirror percentage", route: &networking.HTTPRoute{
			MirrorPercentage: &networking.Percent{
				Value: -1,
			},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "delegate route with delegate", route: &networking.HTTPRoute{
			Delegate: &networking.Delegate{
				Name:      "test",
				Namespace: "test",
			},
		}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateHTTPRoute(tc.route, true); (err.Err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err.Err == nil, tc.valid, err)
			}
		})
	}
}
