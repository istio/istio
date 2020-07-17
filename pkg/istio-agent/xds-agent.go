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
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"istio.io/istio/pilot/pkg/networking/apigen"
	"istio.io/istio/pilot/pkg/networking/grpcgen"
	envoyv2 "istio.io/istio/pilot/pkg/xds/v2"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/security"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/xds"
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
	savePath = env.RegisterStringVar("XDS_SAVE", "",
		"If set, the XDS proxy will save a snapshot of the received config")

	// Can be used by Envoy or istioctl debug tools. Recommended value: 127.0.0.1:15002
	xdsAddr = env.RegisterStringVar("XDS_LOCAL", "",
		"Address for a local XDS proxy. If empty, the proxy is disabled")

	// To configure envoy to use the proxy we'll set discoveryAddress to localhost
	xdsUpstream = env.RegisterStringVar("XDS_UPSTREAM", "",
		"Address for upstream XDS server. If not set, discoveryAddress is used")
)

// initXDS starts an XDS proxy server, using the adsc connection.
// Note that using 'xds.NewXDS' will create a generating server - i.e.
// adsc would be used to get MCP-over-XDS, and the server would generate
// configs.
func (sa *Agent) initXDS() {
	if sa.cfg.LocalXDSAddr == "" {
		sa.cfg.LocalXDSAddr = xdsAddr.Get() // default using the env.
	}
	if sa.cfg.LocalXDSAddr == "" {
		return // not enabled
	}
	s := xds.NewXDS()
	sa.xdsServer = s

	p := s.NewProxy()
	sa.proxyGen = p
	// Configure the XDS server running in istio-agent.
	// Code is shared with Istiod - meaning the internal connection handling, metrics, auth are common
	// However we configure the generators differently - the MCP (API) generator for Istioctl uses the in-memory
	// store, and should work the same with Istiod.
	// TODO: forward any unknown request to the ADSC client, as a default
	// -
	g := s.DiscoveryServer.Generators
	g["grpc"] = &grpcgen.GrpcConfigGenerator{}
	epGen := &xds.EdsGenerator{Server: s.DiscoveryServer}
	g["grpc/"+envoyv2.EndpointType] = epGen
	g["api"] = &apigen.APIGenerator{}
	g["api/"+envoyv2.EndpointType] = epGen

	g[xds.TypeURLConnections] = p
	g[envoyv2.ClusterType] = p
	g[v3.ClusterType] = p

	// GrpcServer server over UDS, shared by SDS and XDS
	sa.secOpts.GrpcServer = grpc.NewServer()

	var err error
	sa.localListener, err = net.Listen("tcp", sa.cfg.LocalXDSAddr)
	if err != nil {
		log.Errorf("Failed to set up TCP path: %v", err)
	}
	sa.localGrpcServer = grpc.NewServer()
	s.DiscoveryServer.Register(sa.localGrpcServer)
	reflection.Register(sa.localGrpcServer)
	sa.LocalXDSListener = sa.localListener
}

// startXDS will start the XDS proxy and client. Will connect to Istiod (or XDS server),
// and start fetching configs to be cached.
// If 'RequireCerts' is set, will attempt to get certificates. Will then attempt to connect to
// the XDS server (istiod), and fetch the initial config. Once the config is ready, will start the
// local XDS proxy and return.
func (sa *Agent) startXDS(proxyConfig *meshconfig.ProxyConfig, secrets security.SecretManager) error {
	if sa.cfg.LocalXDSAddr == "" {
		return nil
	}
	// Same as getPilotSan
	addr := xdsUpstream.Get()
	if addr == "" {
		addr = proxyConfig.DiscoveryAddress
	}
	discHost := strings.Split(addr, ":")[0]
	if discHost == "localhost" {
		discHost = "istiod.istio-system.svc"
	}

	cfg := &adsc.Config{
		XDSSAN:          discHost,
		ResponseHandler: sa.proxyGen,
	}

	// Set Secrets and JWTPath if the default ControlPlaneAuthPolicy is MUTUAL_TLS
	if sa.proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
		cfg.Secrets = secrets
		cfg.JWTPath = sa.secOpts.JWTPath
	}

	ads, err := adsc.New(proxyConfig, cfg)
	if err != nil {
		// Error to be handled by caller - probably by exit if
		// we are in 'envoy using proxy' mode.
		return err
	}

	ads.LocalCacheDir = savePath.Get()
	ads.Store = sa.xdsServer.MemoryConfigStore
	ads.Registry = sa.xdsServer.DiscoveryServer.MemRegistry

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

	if sa.localListener != nil {
		go func() {
			if err := sa.localGrpcServer.Serve(sa.localListener); err != nil {
				log.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
			}
		}()
	}
	return nil
}
