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

package gateway

import (
	"testing"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
)

func TestIsTLSServer(t *testing.T) {
	cases := []struct {
		name     string
		server   *v1alpha3.Server
		expected bool
	}{
		{
			name: "tls non nil and HTTP as transport protocol",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTP),
					Name:     "http",
				},
				Tls: &v1alpha3.ServerTLSSettings{HttpsRedirect: true},
			},
			expected: false,
		},
		{
			name: "tls non nil and TCP as transport protocol",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.TCP),
					Name:     "tcp",
				},
				Tls: &v1alpha3.ServerTLSSettings{HttpsRedirect: true},
			},
			expected: true,
		},
		{
			name: "tls nil and HTTP as transport protocol",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTP),
					Name:     "http",
				},
			},
			expected: false,
		},
		{
			name: "tls nil and TCP as transport protocol",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.TCP),
					Name:     "tcp",
				},
			},
			expected: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := IsTLSServer(tc.server)
			if actual != tc.expected {
				t.Errorf("IsTLSServer(%s) => %t, want %t",
					tc.server, actual, tc.expected)
			}
		})
	}
}

func TestIsHTTPServer(t *testing.T) {
	cases := []struct {
		name     string
		server   *v1alpha3.Server
		expected bool
	}{
		{
			name: "HTTP as transport protocol",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTP),
					Name:     "http",
				},
			},
			expected: true,
		},
		{
			name: "HTTPS traffic with passthrough ServerTLS mode",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTPS),
					Name:     "https",
				},
				Tls: &v1alpha3.ServerTLSSettings{Mode: v1alpha3.ServerTLSSettings_PASSTHROUGH},
			},
			expected: false,
		},
		{
			name: "HTTP traffic with passthrough ServerTLS mode",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTP),
					Name:     "http",
				},
				Tls: &v1alpha3.ServerTLSSettings{Mode: v1alpha3.ServerTLSSettings_PASSTHROUGH},
			},
			expected: true,
		},
		{
			name: "HTTPS traffic with istio mutual ServerTLS mode",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTPS),
					Name:     "https",
				},
				Tls: &v1alpha3.ServerTLSSettings{Mode: v1alpha3.ServerTLSSettings_ISTIO_MUTUAL},
			},
			expected: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := IsHTTPServer(tc.server)
			if actual != tc.expected {
				t.Errorf("IsHTTPServer(%s) => %t, want %t",
					tc.server, actual, tc.expected)
			}
		})
	}
}

func TestIsEligibleForHTTP3Upgrade(t *testing.T) {
	cases := []struct {
		name                string
		server              *v1alpha3.Server
		enableQUICListeners bool
		expected            bool
	}{
		{
			name: "EnableQUICListeners set to false",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTP),
					Name:     "http",
				},
			},
			expected:            false,
			enableQUICListeners: false,
		},
		{
			name: "HTTP as transport protocol and EnableQUICListeners set to true",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTP),
					Name:     "http",
				},
			},
			expected:            false,
			enableQUICListeners: true,
		},
		{
			name: "HTTPS traffic with passthrough ServerTLS mode and EnableQUICListeners set to true",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTPS),
					Name:     "https",
				},
				Tls: &v1alpha3.ServerTLSSettings{Mode: v1alpha3.ServerTLSSettings_PASSTHROUGH},
			},
			enableQUICListeners: true,
			expected:            false,
		},
		{
			name: "HTTPS traffic with istio mutual ServerTLS mode and EnableQUICListeners set to true",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTPS),
					Name:     "https",
				},
				Tls: &v1alpha3.ServerTLSSettings{Mode: v1alpha3.ServerTLSSettings_ISTIO_MUTUAL},
			},
			enableQUICListeners: true,
			expected:            true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			test.SetBoolForTest(t, &features.EnableQUICListeners, tc.enableQUICListeners)
			actual := IsEligibleForHTTP3Upgrade(tc.server)
			if actual != tc.expected {
				t.Errorf("IsEligibleForHTTP3Upgrade(%s) => %t, want %t",
					tc.server, actual, tc.expected)
			}
		})
	}
}

func TestIsPassThroughServer(t *testing.T) {
	cases := []struct {
		name     string
		server   *v1alpha3.Server
		expected bool
	}{
		{
			name: "nil server TlS",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTP),
					Name:     "http",
				},
			},
			expected: false,
		},
		{
			name: "passthrough ServerTLS mode",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTPS),
					Name:     "https",
				},
				Tls: &v1alpha3.ServerTLSSettings{Mode: v1alpha3.ServerTLSSettings_PASSTHROUGH},
			},
			expected: true,
		},
		{
			name: "auto passthrough ServerTLS mode",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTP),
					Name:     "http",
				},
				Tls: &v1alpha3.ServerTLSSettings{Mode: v1alpha3.ServerTLSSettings_AUTO_PASSTHROUGH},
			},
			expected: true,
		},
		{
			name: "istio mutual ServerTLS mode",
			server: &v1alpha3.Server{
				Port: &v1alpha3.Port{
					Number:   80,
					Protocol: string(protocol.HTTPS),
					Name:     "https",
				},
				Tls: &v1alpha3.ServerTLSSettings{Mode: v1alpha3.ServerTLSSettings_ISTIO_MUTUAL},
			},
			expected: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := IsPassThroughServer(tc.server)
			if actual != tc.expected {
				t.Errorf("IsPassThroughServer(%s) => %t, want %t",
					tc.server, actual, tc.expected)
			}
		})
	}
}
