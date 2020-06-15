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

package istioagent

import (
	"net"
	"time"

	"google.golang.org/grpc"
	"istio.io/istio/pilot/pkg/networking/apigen"
	"istio.io/istio/pilot/pkg/networking/grpcgen"
	envoyv2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	v3 "istio.io/istio/pilot/pkg/proxy/envoy/v3"

	"istio.io/istio/security/pkg/nodeagent/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/proxy/envoy/xds"
	"istio.io/istio/pkg/adsc"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

// xds-agent runs an XDS server in agent, similar with the SDS server.
//

var (
	// Used for debugging. Will also be used to recover in case XDS is unavailable, and to evaluate the
	// caching. Key format and content should follow the new resource naming spec, will evolve with the spec.
	// As Envoy implements the spec, they should be used directly by Envoy - while prototyping they can
	// be handled by the proxy.
	savePath = env.RegisterStringVar("XDS_SAVE", "./var/istio/xds",
		"If set, the XDS proxy will save a snapshot of the received config")
)

var (
	// Can be used by Envoy or istioctl debug tools.
	xdsAddr = env.RegisterStringVar("XDS_LOCAL", "127.0.0.1:15002",
		"Address for a local XDS proxy. If empty, the proxy is disabled")

	// used for XDS portion.
	localListener   net.Listener
	localGrpcServer *grpc.Server

	xdsServer *xds.Server
	cfg       *meshconfig.ProxyConfig
)

// initXDS starts an XDS proxy server, using the adsc connection.
// Note that using 'xds.NewXDS' will create a generating server - i.e.
// adsc would be used to get MCP-over-XDS, and the server would generate
// configs.
func (sa *Agent) initXDS(mc *meshconfig.ProxyConfig) {
	s := xds.NewXDS()
	xdsServer = s
	cfg = mc

	p := s. NewProxy()
	sa.proxyGen = p
	// Configure the XDS server running in istio-agent.
	// Code is shared with Istiod - meaning the internal connection handling, metrics, auth are common
	// However we configure the generators differently - the MCP (API) generator for Istioctl uses the in-memory
	// store, and should work the same with Istiod.
	// TODO: forward any unknown request to the ADSC client, as a default
	// -
	g := s.DiscoveryServer.Generators
	// To allow gRPC clients to connect to localhost, avoiding TLS complexity
	g["grpc"] = &grpcgen.GrpcConfigGenerator{}
	epGen := &envoyv2.EdsGenerator{Server: s.DiscoveryServer}
	g["grpc/"+envoyv2.EndpointType] = epGen
	g["api"] = &apigen.APIGenerator{}
	g["api/"+envoyv2.EndpointType] = epGen

	g[envoyv2.TypeURLConnections] = p
	g[envoyv2.ClusterType] = p
	g[v3.ClusterType] = p

	// GrpcServer server over UDS, shared by SDS and XDS
	serverOptions.GrpcServer = grpc.NewServer()

	var err error
	if sa.LocalXDSAddr == "" {
		sa.LocalXDSAddr = xdsAddr.Get() // default using the env.
	}
	if sa.LocalXDSAddr != "" {
		localListener, err = net.Listen("tcp", sa.LocalXDSAddr)
		if err != nil {
			log.Errorf("Failed to set up TCP path: %v", err)
		}
		localGrpcServer = grpc.NewServer()
		s.DiscoveryServer.Register(localGrpcServer)
		sa.LocalXDSListener = localListener
	}
}

// startXDS will start the XDS proxy and client. Will connect to Istiod (or XDS server),
// and start fetching configs to be cached.
func (sa *Agent) startXDS(proxyConfig *meshconfig.ProxyConfig, secrets cache.SecretManager) error {
	// TODO: handle certificates and security - similar with the
	// code we generate for envoy !

	ads, err := adsc.Dial(proxyConfig.DiscoveryAddress,
		"",
		&adsc.Config{
			Secrets: secrets,
		})
	if err != nil {
		// Exit immediately - the XDS server is not reachable. The sidecar should restart.
		// TODO: we can also return an error, but eventually it should still exit - and let
		// k8s report and deal with restarts.
		log.Fatala("Failed to connect to XDS server ", err)
	}

	ads.LocalCacheDir = savePath.Get()
	ads.Store = xdsServer.MemoryConfigStore
	ads.Registry = xdsServer.DiscoveryServer.MemRegistry

	// Send requests for MCP configs, for caching/debugging.
	ads.WatchConfig()

	// Send requests for normal envoy configs
	ads.Watch()

	sa.proxyGen.AddClient(ads)

	syncOk := ads.WaitConfigSync(10 * time.Second)
	if !syncOk {
		// TODO: have the store return a sync map, or better expose sync events/status
		log.Warna("Incomplete sync")
	}

	// TODO: wait for config to sync before starting the rest
	// TODO: handle push

	if localListener != nil {
		go func() {
			if err := localGrpcServer.Serve(localListener); err != nil {
				log.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
			}
		}()
	}
	return nil
}
