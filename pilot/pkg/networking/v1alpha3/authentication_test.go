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

package v1alpha3

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/duration"

	authn "istio.io/api/authentication/v1alpha2"
)

func TestBuildJwtFilter(t *testing.T) {
	cases := []struct {
		in       *authn.Policy
		expected *http_conn.HttpFilter
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
			expected: &http_conn.HttpFilter{
				Name: "jwt-auth",
				Config: &types.Struct{
					Fields: map[string]*types.Value{
						"jwts": {
							Kind: &types.Value_ListValue{
								ListValue: &types.ListValue{
									Values: []*types.Value{
										{
											Kind: &types.Value_StructValue{
												StructValue: &types.Struct{
													Fields: map[string]*types.Value{
														"forward_jwt": {
															Kind: &types.Value_BoolValue{BoolValue: true},
														},
														"jwks_uri": {
															Kind: &types.Value_StringValue{
																StringValue: "http://abc.com",
															},
														},
														"jwks_uri_envoy_cluster": {
															Kind: &types.Value_StringValue{
																StringValue: "jwks.abc.com|http",
															},
														},
														"public_key_cache_duration": {
															Kind: &types.Value_StringValue{
																StringValue: "300.000s",
															},
														},
													},
												},
											},
										},
									},
								},
							},
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

const (
	goldenClusterAbc = `name:"jwks.abc.com|http"
	type:STRICT_DNS connect_timeout:<seconds:42 >
	hosts:<socket_address:<address:"abc.com" port_value:80 > >`
	goldenClusterXyz = `name:"jwks.xyz.com|https"
	type:STRICT_DNS connect_timeout:<seconds:42 >
	hosts:<socket_address:<address:"xyz.com" port_value:443 > >`
)

func makeGoldenCluster(text string) *v2.Cluster {
	cluster := &v2.Cluster{}
	if err := proto.UnmarshalText(text, cluster); err != nil {
		panic(fmt.Sprintf("Cannot parse golden cluster data %s", text))
	}
	return cluster
}

func TestBuildJwksURIClusters(t *testing.T) {
	cases := []struct {
		name     string
		in       []*authn.Jwt
		expected map[string]*v2.Cluster
	}{
		{
			name:     "nil list",
			in:       nil,
			expected: map[string]*v2.Cluster{},
		},
		{
			name:     "empty list",
			in:       []*authn.Jwt{},
			expected: map[string]*v2.Cluster{},
		},
		{
			name: "one jwt policy",
			in: []*authn.Jwt{
				{
					JwksUri: "http://abc.com",
				},
			},
			expected: map[string]*v2.Cluster{
				"jwks.abc.com|http": makeGoldenCluster(goldenClusterAbc),
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
			expected: map[string]*v2.Cluster{
				"jwks.abc.com|http":  makeGoldenCluster(goldenClusterAbc),
				"jwks.xyz.com|https": makeGoldenCluster(goldenClusterXyz),
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
			expected: map[string]*v2.Cluster{
				"jwks.abc.com|http":  makeGoldenCluster(goldenClusterAbc),
				"jwks.xyz.com|https": makeGoldenCluster(goldenClusterXyz),
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
				t.Errorf("Test case %s: expected\n%s\n, got\n%s", c.name, expectedCluster.String(), cluster.String())

			}
		}
	}
}
