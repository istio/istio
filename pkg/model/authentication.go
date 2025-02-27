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

package model

import (
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	// SDSClusterName is the name of the cluster for SDS connections
	SDSClusterName     = "sds-grpc"
	SDSFileClusterName = "sds-files-grpc"

	// SDSDefaultResourceName is the default name in sdsconfig, used for fetching normal key/cert.
	SDSDefaultResourceName = "default"

	// SDSRootResourceName is the sdsconfig name for root CA, used for fetching root cert.
	SDSRootResourceName = "ROOTCA"
)

// Preconfigured SDS configs to avoid excessive memory allocations
var (
	defaultSDSConfig = &tls.SdsSecretConfig{
		Name: SDSDefaultResourceName,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:                   core.ApiConfigSource_GRPC,
					SetNodeOnFirstMessageOnly: true,
					TransportApiVersion:       core.ApiVersion_V3,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
							},
						},
					},
				},
			},
			ResourceApiVersion:  core.ApiVersion_V3,
			InitialFetchTimeout: durationpb.New(time.Second * 0),
		},
	}
	rootSDSConfig = &tls.SdsSecretConfig{
		Name: SDSRootResourceName,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:                   core.ApiConfigSource_GRPC,
					SetNodeOnFirstMessageOnly: true,
					TransportApiVersion:       core.ApiVersion_V3,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
							},
						},
					},
				},
			},
			ResourceApiVersion:  core.ApiVersion_V3,
			InitialFetchTimeout: durationpb.New(time.Second * 0),
		},
	}
)

// ConstructSdsSecretConfig constructs SDS Secret Configuration for workload proxy.
func ConstructSdsSecretConfig(name string) *tls.SdsSecretConfig {
	return constructSdsSecretConfig(name, SDSClusterName)
}

// ConstructSdsFilesSecretConfig constructs SDS Secret Configuration for workload proxy, using a custom path for files
func ConstructSdsFilesSecretConfig(name string) *tls.SdsSecretConfig {
	return constructSdsSecretConfig(name, SDSFileClusterName)
}

func constructSdsSecretConfig(name string, clusterName string) *tls.SdsSecretConfig {
	if name == "" {
		return nil
	}

	if name == SDSDefaultResourceName {
		return defaultSDSConfig
	}
	if name == SDSRootResourceName {
		return rootSDSConfig
	}

	cfg := &tls.SdsSecretConfig{
		Name: name,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					SetNodeOnFirstMessageOnly: true,
					ApiType:                   core.ApiConfigSource_GRPC,
					TransportApiVersion:       core.ApiVersion_V3,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: clusterName},
							},
						},
					},
				},
			},
			ResourceApiVersion: core.ApiVersion_V3,
		},
	}

	return cfg
}
