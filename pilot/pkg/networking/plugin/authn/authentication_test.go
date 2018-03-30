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

package authn

import (
	"reflect"
	"testing"

	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/gogo/protobuf/types"

	authn "istio.io/api/authentication/v1alpha1"
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
		if got := BuildJwtFilter(c.in); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("buildJwtFilter(%#v), got:\n%#v\nwanted:\n%#v\n", c.in, got, c.expected)
		}
	}
}
