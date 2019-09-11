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
	"fmt"
	"reflect"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/config/grpc_credential/v2alpha"
	"istio.io/istio/pilot/pkg/networking/util"

	"istio.io/istio/pilot/pkg/features"
)

func TestConstructSdsSecretConfig(t *testing.T) {
	trustworthyMetaConfig := &v2alpha.FileBasedMetadataConfig{
		SecretData: &core.DataSource{
			Specifier: &core.DataSource_Filename{
				Filename: K8sSATrustworthyJwtFileName,
			},
		},
		HeaderKey: K8sSAJwtTokenHeaderKey,
	}

	cases := []struct {
		serviceAccount string
		sdsUdsPath     string
		expected       *auth.SdsSecretConfig
		metadata       map[string]string
	}{
		{
			serviceAccount: "spiffe://cluster.local/ns/bar/sa/foo",
			sdsUdsPath:     "/tmp/sdsuds.sock",
			expected: &auth.SdsSecretConfig{
				Name:      "spiffe://cluster.local/ns/bar/sa/foo",
				SdsConfig: constructSdsConfigHelper(trustworthyMetaConfig),
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
		if got := ConstructSdsSecretConfig(c.serviceAccount, c.sdsUdsPath, c.metadata); !reflect.DeepEqual(got, c.expected) {
			t.Errorf("ConstructSdsSecretConfig: got(%#v) != want(%#v)\n", got, c.expected)
			fmt.Println(got)
			fmt.Println(c.expected)
		}
	}
}

func TestConstructSdsSecretConfigForGatewayListener(t *testing.T) {
	cases := []struct {
		serviceAccount string
		sdsUdsPath     string
		expected       *auth.SdsSecretConfig
	}{
		{
			serviceAccount: "spiffe://cluster.local/ns/bar/sa/foo",
			sdsUdsPath:     "/tmp/sdsuds.sock",
			expected: &auth.SdsSecretConfig{
				Name: "spiffe://cluster.local/ns/bar/sa/foo",
				SdsConfig: &core.ConfigSource{
					InitialFetchTimeout: features.InitialFetchTimeout,
					ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
						ApiConfigSource: &core.ApiConfigSource{
							ApiType: core.ApiConfigSource_GRPC,
							GrpcServices: []*core.GrpcService{
								{
									TargetSpecifier: &core.GrpcService_GoogleGrpc_{
										GoogleGrpc: &core.GrpcService_GoogleGrpc{
											TargetUri:  "/tmp/sdsuds.sock",
											StatPrefix: SDSStatPrefix,
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
			serviceAccount: "spiffe://cluster.local/ns/bar/sa/foo",
			sdsUdsPath:     "",
			expected:       nil,
		},
	}

	for _, c := range cases {
		if got := ConstructSdsSecretConfigForGatewayListener(c.serviceAccount, c.sdsUdsPath); !reflect.DeepEqual(got, c.expected) {
			t.Errorf("ConstructSdsSecretConfig: got(%#v) != want(%#v)\n", got, c.expected)
		}
	}
}

func constructLocalChannelCredConfig() *core.GrpcService_GoogleGrpc_ChannelCredentials {
	return &core.GrpcService_GoogleGrpc_ChannelCredentials{
		CredentialSpecifier: &core.GrpcService_GoogleGrpc_ChannelCredentials_LocalCredentials{
			LocalCredentials: &core.GrpcService_GoogleGrpc_GoogleLocalCredentials{},
		},
	}
}

func constructSdsConfigHelper(metaConfig *v2alpha.FileBasedMetadataConfig) *core.ConfigSource { // nolint:interfacer
	return &core.ConfigSource{
		InitialFetchTimeout: features.InitialFetchTimeout,
		ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
			ApiConfigSource: &core.ApiConfigSource{
				ApiType: core.ApiConfigSource_GRPC,
				GrpcServices: []*core.GrpcService{
					{
						TargetSpecifier: &core.GrpcService_GoogleGrpc_{
							GoogleGrpc: &core.GrpcService_GoogleGrpc{
								TargetUri:              "/tmp/sdsuds.sock",
								StatPrefix:             SDSStatPrefix,
								CredentialsFactoryName: FileBasedMetadataPlugName,
								ChannelCredentials:     constructLocalChannelCredConfig(),
								CallCredentials: []*core.GrpcService_GoogleGrpc_CallCredentials{
									{
										CredentialSpecifier: &core.GrpcService_GoogleGrpc_CallCredentials_FromPlugin{
											FromPlugin: &core.GrpcService_GoogleGrpc_CallCredentials_MetadataCredentialsFromPlugin{
												Name: "envoy.grpc_credentials.file_based_metadata",
												ConfigType: &core.GrpcService_GoogleGrpc_CallCredentials_MetadataCredentialsFromPlugin_Config{
													Config: util.MessageToStruct(metaConfig)},
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
	}
}
