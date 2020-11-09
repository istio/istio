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

package configdump

import (
	"testing"

	v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
)

func TestListenerFilter_Verify(t *testing.T) {
	tests := []struct {
		desc       string
		inFilter   *ListenerFilter
		inListener *listener.Listener
		expect     bool
	}{
		{
			desc: "filter-fields-empty",
			inFilter: &ListenerFilter{
				Address: "",
				Port:    0,
				Type:    "",
			},
			inListener: &listener.Listener{},
			expect:     true,
		},
		{
			desc: "addrs-dont-match",
			inFilter: &ListenerFilter{
				Address: "0.0.0.0",
			},
			inListener: &listener.Listener{
				Address: &v3.Address{
					Address: &v3.Address_SocketAddress{
						SocketAddress: &v3.SocketAddress{Address: "1.1.1.1"},
					},
				},
			},
			expect: false,
		},
		{
			desc: "ports-dont-match",
			inFilter: &ListenerFilter{
				Port: 10,
			},
			inListener: &listener.Listener{
				Address: &v3.Address{
					Address: &v3.Address_SocketAddress{
						SocketAddress: &v3.SocketAddress{
							PortSpecifier: &v3.SocketAddress_PortValue{
								PortValue: 11,
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			desc: "http-type-match",
			inFilter: &ListenerFilter{
				Type: "HTTP",
			},
			inListener: &listener.Listener{
				FilterChains: []*listener.FilterChain{{
					Filters: []*listener.Filter{{
						Name: wellknown.HTTPConnectionManager,
					},
					},
				},
				},
			},
			expect: true,
		},
		{
			desc: "http-tcp-type-match",
			inFilter: &ListenerFilter{
				Type: "HTTP+TCP",
			},
			inListener: &listener.Listener{
				FilterChains: []*listener.FilterChain{{
					Filters: []*listener.Filter{{
						Name: wellknown.TCPProxy,
					},
						{
							Name: wellknown.TCPProxy,
						},
						{
							Name: wellknown.HTTPConnectionManager,
						}},
				}},
			},
			expect: true,
		},
		{
			desc: "tcp-type-match",
			inFilter: &ListenerFilter{
				Type: "TCP",
			},
			inListener: &listener.Listener{
				FilterChains: []*listener.FilterChain{{
					Filters: []*listener.Filter{{
						Name: wellknown.TCPProxy,
					}},
				}},
			},
			expect: true,
		},
		{
			desc: "unknown-type",
			inFilter: &ListenerFilter{
				Type: "UNKNOWN",
			},
			inListener: &listener.Listener{
				FilterChains: []*listener.FilterChain{{
					Filters: []*listener.Filter{},
				}},
			},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := tt.inFilter.Verify(tt.inListener); got != tt.expect {
				t.Errorf("%s: expect %v got %v", tt.desc, tt.expect, got)
			}
		})
	}
}
