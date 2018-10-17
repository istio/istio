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
	"os"
	"reflect"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/config/grpc_credential/v2alpha"
	"github.com/gogo/protobuf/types"
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

func TestConstructSdsSecretConfig(t *testing.T) {
	testTokenFileName := "/tmp/randomtestFile"
	if _, err := os.Stat(testTokenFileName); os.IsNotExist(err) {
		// create test file if not exist
		if _, err := os.Create(testTokenFileName); err != nil {
			log.Fatal("failed for test token file")
		}
		defer os.Remove(testTokenFileName)
	}

	metaConfig := &v2alpha.FileBasedMetadataConfig{
		SecretData: &core.DataSource{
			Specifier: &core.DataSource_Filename{
				Filename: testTokenFileName,
			},
		},
		HeaderKey: k8sSAJwtTokenHeaderKey,
	}

	cases := []struct {
		serviceAccount string
		sdsUdsPath     string
		tokenFilePath  string
		expected       *auth.SdsSecretConfig
	}{
		{
			serviceAccount: "spiffe://cluster.local/ns/bar/sa/foo",
			sdsUdsPath:     "/tmp/sdsuds.sock",
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
											TargetUri:          "/tmp/sdsuds.sock",
											StatPrefix:         SDSStatPrefix,
											ChannelCredentials: constructLocalChannelredConfig(),
											CallCredentials: []*core.GrpcService_GoogleGrpc_CallCredentials{
												constructGCECallCredConfig(),
											},
										},
									},
								},
							},
							RefreshDelay: nil,
						},
					},
				},
			},
		},
		{
			serviceAccount: "spiffe://cluster.local/ns/bar/sa/foo",
			sdsUdsPath:     "/tmp/sdsuds.sock",
			tokenFilePath:  testTokenFileName,
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
											TargetUri:              "/tmp/sdsuds.sock",
											StatPrefix:             SDSStatPrefix,
											CredentialsFactoryName: "envoy.grpc_credentials.file_based_metadata",
											ChannelCredentials:     constructLocalChannelredConfig(),
											CallCredentials: []*core.GrpcService_GoogleGrpc_CallCredentials{
												&core.GrpcService_GoogleGrpc_CallCredentials{
													CredentialSpecifier: &core.GrpcService_GoogleGrpc_CallCredentials_FromPlugin{
														FromPlugin: &core.GrpcService_GoogleGrpc_CallCredentials_MetadataCredentialsFromPlugin{
															Name:   "envoy.grpc_credentials.file_based_metadata",
															Config: protoToStruct(metaConfig),
														},
													},
												},
											},
										},
									},
								},
							},
							RefreshDelay: nil,
						},
					},
				},
			},
		},
		{
			serviceAccount: "",
			sdsUdsPath:     "/tmp/sdsuds.sock",
			expected:       nil,
		},
		{
			serviceAccount: "",
			sdsUdsPath:     "spiffe://cluster.local/ns/bar/sa/foo",
			expected:       nil,
		},
	}

	for _, c := range cases {
		if got := ConstructSdsSecretConfig(c.serviceAccount, c.sdsUdsPath, c.tokenFilePath); !reflect.DeepEqual(got, c.expected) {
			t.Errorf("ConstructSdsSecretConfig: got(%#v) != want(%#v)\n", got, c.expected)
		}
	}
}

func constructLocalChannelredConfig() *core.GrpcService_GoogleGrpc_ChannelCredentials {
	return &core.GrpcService_GoogleGrpc_ChannelCredentials{
		CredentialSpecifier: &core.GrpcService_GoogleGrpc_ChannelCredentials_LocalCredentials{
			LocalCredentials: &core.GrpcService_GoogleGrpc_GoogleLocalCredentials{},
		},
	}
}

func constructGCECallCredConfig() *core.GrpcService_GoogleGrpc_CallCredentials {
	return &core.GrpcService_GoogleGrpc_CallCredentials{
		CredentialSpecifier: &core.GrpcService_GoogleGrpc_CallCredentials_GoogleComputeEngine{
			GoogleComputeEngine: &types.Empty{},
		},
	}
}
