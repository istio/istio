// Copyright 2018 Istio Authors
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

package v1

import (
	"reflect"
	"testing"

	"github.com/golang/protobuf/ptypes/duration"

	authn "istio.io/api/authentication/v1alpha2"
	"istio.io/istio/pilot/pkg/model"
)

func TestBuildJwksFilter(t *testing.T) {
	cases := []struct {
		in       *authn.Policy
		expected *HTTPFilter
	}{
		{
			in:       nil,
			expected: nil,
		},
		{
			in:       nil,
			expected: nil,
		},
		{
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{
							Jwt: &authn.Jwt{
								JwksUri: "http://abc.com",
							},
						},
					},
				},
			},
			expected: &HTTPFilter{
				Type: "decoder",
				Name: "jwt-auth",
				Config: map[string]interface{}{
					"jwts": []interface{}{
						map[string]interface{}{
							"jwksUri":                "http://abc.com",
							"forwardJwt":             true,
							"publicKeyCacheDuration": "300.000s",
							"jwksUriEnvoyCluster":    "jwks.abc.com|http",
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		if got := buildJwtFilter(c.in); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("buildJwtFilter(%#v), got:\n%#v\nwanted:\n%#v\n", c.in, got, c.expected)
		}
	}
}

func makeGoldenCluster(mode int) *Cluster {
	var name, host, serviceName, hostname string
	var port *model.Port
	var ssl interface{}

	if mode == 0 {
		name = "jwks.abc.com|http"
		serviceName = "abc.com|http"
		host = "tcp://abc.com:80"
		hostname = "abc.com"
		port = &model.Port{
			Name: "http",
			Port: 80,
		}
	} else {
		name = "jwks.xyz.com|https"
		serviceName = "xyz.com|https"
		host = "tcp://xyz.com:443"
		hostname = "xyz.com"
		port = &model.Port{
			Name: "https",
			Port: 443,
		}
		ssl = &SSLContextExternal{CaCertFile: ""}
	}

	return &Cluster{
		Name:             name,
		ServiceName:      serviceName,
		ConnectTimeoutMs: 42000,
		Type:             "strict_dns",
		LbType:           "round_robin",
		MaxRequestsPerConnection: 0,
		Hosts: []Host{
			{URL: host},
		},
		SSLContext: ssl,
		Features:   "",
		CircuitBreaker: &CircuitBreaker{
			Default: DefaultCBPriority{
				MaxConnections:     0,
				MaxPendingRequests: 10000,
				MaxRequests:        10000,
				MaxRetries:         0,
			},
		},
		OutlierDetection: nil,
		outbound:         false,
		Hostname:         hostname,
		Port:             port,
		labels:           nil,
	}
}

func TestBuildJwksURIClusters(t *testing.T) {
	cases := []struct {
		name     string
		in       []*authn.Jwt
		expected map[string]*Cluster
	}{
		{
			name:     "nil list",
			in:       nil,
			expected: map[string]*Cluster{},
		},
		{
			name:     "empty list",
			in:       []*authn.Jwt{},
			expected: map[string]*Cluster{},
		},
		{
			name: "one jwt policy",
			in: []*authn.Jwt{
				{
					JwksUri: "http://abc.com",
				},
			},
			expected: map[string]*Cluster{
				"jwks.abc.com|http": makeGoldenCluster(0),
			},
		},
		{
			name: "two jwt policy",
			in: []*authn.Jwt{
				{
					JwksUri: "http://abc.com",
				},
				{
					JwksUri: "https://xyz.com",
				},
			},
			expected: map[string]*Cluster{
				"jwks.abc.com|http":  makeGoldenCluster(0),
				"jwks.xyz.com|https": makeGoldenCluster(1),
			},
		},
		{
			name: "duplicate jwt policy",
			in: []*authn.Jwt{
				{
					JwksUri: "http://abc.com",
				},
				{
					JwksUri: "https://xyz.com",
				},
				{
					JwksUri: "http://abc.com",
				},
			},
			expected: map[string]*Cluster{
				"jwks.abc.com|http":  makeGoldenCluster(0),
				"jwks.xyz.com|https": makeGoldenCluster(1),
			},
		},
	}
	for _, c := range cases {
		got := buildJwksURIClusters(c.in, &duration.Duration{Seconds: 42})
		if len(got) != len(c.expected) {
			t.Errorf("collectJwtSpecs(%#v): return (%d) != want(%d)\n", c.in, len(got), len(c.expected))
		}
		for _, cluster := range got {
			expectedCluster := c.expected[cluster.Name]
			if !reflect.DeepEqual(expectedCluster, cluster) {
				t.Errorf("Test case %s: expected\n%#v\n, got\n%#v", c.name, expectedCluster, cluster)

			}
		}
	}
}
