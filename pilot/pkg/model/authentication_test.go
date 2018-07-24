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

package model

import (
	"reflect"
	"testing"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/types"

	authn "istio.io/api/authentication/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
)

func TestParseJwksURI(t *testing.T) {
	cases := []struct {
		in                   string
		expectedHostname     string
		expectedPort         *Port
		expectedUseSSL       bool
		expectedErrorMessage string
	}{
		{
			in:                   "foo.bar.com",
			expectedErrorMessage: `URI scheme "" is not supported`,
		},
		{
			in:                   "tcp://foo.bar.com:abc",
			expectedErrorMessage: `URI scheme "tcp" is not supported`,
		},
		{
			in:                   "http://foo.bar.com:abc",
			expectedErrorMessage: `strconv.Atoi: parsing "abc": invalid syntax`,
		},
		{
			in:               "http://foo.bar.com",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "http", Port: 80},
			expectedUseSSL:   false,
		},
		{
			in:               "https://foo.bar.com",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "https", Port: 443},
			expectedUseSSL:   true,
		},
		{
			in:               "http://foo.bar.com:1234",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "http", Port: 1234},
			expectedUseSSL:   false,
		},
		{
			in:               "https://foo.bar.com:1234/secure/key",
			expectedHostname: "foo.bar.com",
			expectedPort:     &Port{Name: "https", Port: 1234},
			expectedUseSSL:   true,
		},
	}
	for _, c := range cases {
		host, port, useSSL, err := ParseJwksURI(c.in)
		if err != nil {
			if c.expectedErrorMessage != err.Error() {
				t.Errorf("ParseJwksURI(%s): expected error (%s), got (%v)", c.in, c.expectedErrorMessage, err)
			}
		} else {
			if c.expectedErrorMessage != "" {
				t.Errorf("ParseJwksURI(%s): expected error (%s), got no error", c.in, c.expectedErrorMessage)
			}
			if c.expectedHostname != host || !reflect.DeepEqual(c.expectedPort, port) || c.expectedUseSSL != useSSL {
				t.Errorf("ParseJwksURI(%s): expected (%s, %#v, %v), got (%s, %#v, %v)",
					c.in, c.expectedHostname, c.expectedPort, c.expectedUseSSL,
					host, port, useSSL)
			}
		}
	}
}

func TestLegacyAuthenticationPolicyToPolicy(t *testing.T) {
	cases := []struct {
		in       meshconfig.MeshConfig_AuthPolicy
		expected *authn.Policy
	}{
		{
			in: meshconfig.MeshConfig_MUTUAL_TLS,
			expected: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{&authn.MutualTls{}},
				}},
			},
		},
		{
			in:       meshconfig.MeshConfig_NONE,
			expected: nil,
		},
	}

	for _, c := range cases {
		if got := legacyAuthenticationPolicyToPolicy(c.in); !reflect.DeepEqual(got, c.expected) {
			t.Errorf("legacyAuthenticationPolicyToPolicy(%v): got(%#v) != want(%#v)\n", c.in, got, c.expected)
		}
	}
}

func TestConstructSdsSecretConfig(t *testing.T) {
	refreshDelay := 15 * time.Second

	cases := []struct {
		serviceAccount  string
		refreshDuration *time.Duration
		sdsUdsPath      string
		expected        *auth.SdsSecretConfig
	}{
		{
			serviceAccount:  "spiffe://cluster.local/ns/bar/sa/foo",
			sdsUdsPath:      "/tmp/sdsuds.sock",
			refreshDuration: &refreshDelay,
			expected: &auth.SdsSecretConfig{
				Name: "spiffe://cluster.local/ns/bar/sa/foo",
				SdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
						ApiConfigSource: &core.ApiConfigSource{
							ApiType: core.ApiConfigSource_GRPC,
							GrpcServices: []*core.GrpcService{
								{
									TargetSpecifier: &core.GrpcService_GoogleGrpc_{
										GoogleGrpc: &core.GrpcService_GoogleGrpc{
											TargetUri:  "/tmp/sdsuds.sock",
											StatPrefix: SDSStatPrefix,
											CallCredentials: []*core.GrpcService_GoogleGrpc_CallCredentials{
												&core.GrpcService_GoogleGrpc_CallCredentials{
													CredentialSpecifier: &core.GrpcService_GoogleGrpc_CallCredentials_GoogleComputeEngine{
														GoogleComputeEngine: &types.Empty{},
													},
												},
											},
										},
									},
								},
							},
							RefreshDelay: &refreshDelay,
						},
					},
				},
			},
		},
		{
			serviceAccount:  "",
			sdsUdsPath:      "/tmp/sdsuds.sock",
			refreshDuration: &refreshDelay,
			expected:        nil,
		},
		{
			serviceAccount:  "",
			sdsUdsPath:      "spiffe://cluster.local/ns/bar/sa/foo",
			refreshDuration: &refreshDelay,
			expected:        nil,
		},
		{
			serviceAccount:  "spiffe://cluster.local/ns/bar/sa/foo",
			sdsUdsPath:      "/tmp/sdsuds.sock",
			refreshDuration: nil,
			expected:        nil,
		},
	}

	for _, c := range cases {
		if got := ConstructSdsSecretConfig(c.serviceAccount, c.refreshDuration, c.sdsUdsPath); !reflect.DeepEqual(got, c.expected) {
			t.Errorf("ConstructSdsSecretConfig: got(%#v) != want(%#v)\n", got, c.expected)
		}
	}
}
