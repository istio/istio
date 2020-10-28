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

package istioagent

import (
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/networking/apigen"
	"istio.io/istio/pilot/pkg/networking/grpcgen"
	"istio.io/istio/pilot/pkg/xds"
	envoyv2 "istio.io/istio/pilot/pkg/xds/v2"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/security"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

// Experimental
// xds-generator runs an XDS generator server in agent, similar with the SDS server.
// It fetches configs from istiod and generates xds configs locally.

var (
	// Experimental. to evaluate the potential for generating all XDS configs locally.
	localXDSGenListenAddr = env.RegisterStringVar("LOCAL_XDS_GENERATOR", "",
		"Address for a local XDS proxy. If empty, the proxy is disabled")

	savePath = env.RegisterStringVar("LOCAL_XDS_GENERATOR_CONFIG_SAVE_PATH", "",
		"If set, the XDS proxy will save a snapshot of the received config")

	xdsUpstream = env.RegisterStringVar("XDS_UPSTREAM", "",
		"Address for upstream XDS server. If not set, discoveryAddress is used")
)

type localXDSGenerator struct {
	// used for local XDS generator portion.
	listener   net.Listener
	grpcServer *grpc.Server
	xdsServer  *xds.SimpleServer
	// ProxyGen is a generator for proxied types - will 'generate' XDS by using
	// an adsc connection.
	proxyGen *xds.ProxyGen
}

// initXDSGenerator starts an XDS proxy server, using the adsc connection.
// Note that using 'xds.NewXDS' will create a generating server - i.e.
// adsc would be used to get MCP-over-XDS, and the server would generate
// configs.
func (sa *Agent) initXDSGenerator() {
	if sa.cfg.LocalXDSGeneratorListenAddress == "" {
		sa.cfg.LocalXDSGeneratorListenAddress = localXDSGenListenAddr.Get() // default using the env.
	}
	if sa.cfg.LocalXDSGeneratorListenAddress == "" {
		return // not enabled
	}
	s := xds.NewXDS()
	p := s.NewProxy()

	sa.localXDSGenerator = &localXDSGenerator{
		xdsServer: s,
		proxyGen:  p,
	}

	// Configure the XDS server running in istio-agent.
	// Code is shared with Istiod - meaning the internal connection handling, metrics, auth are common
	// However we configure the generators differently - the MCP (API) generator for Istioctl uses the in-memory
	// store, and should work the same with Istiod.
	// TODO: forward any unknown request to the ADSC upstream, as a default
	// -
	g := s.DiscoveryServer.Generators
	g["grpc"] = &grpcgen.GrpcConfigGenerator{}
	epGen := &xds.EdsGenerator{Server: s.DiscoveryServer}
	g["grpc/"+envoyv2.EndpointType] = epGen
	g["api"] = &apigen.APIGenerator{}
	g["api/"+envoyv2.EndpointType] = epGen

	g[xds.TypeURLConnections] = p
	g[v3.ClusterType] = p

	var err error
	sa.localXDSGenerator.listener, err = net.Listen("tcp", sa.cfg.LocalXDSGeneratorListenAddress)
	if err != nil {
		log.Errorf("Failed to set up TCP path: %v", err)
	}
	sa.localXDSGenerator.grpcServer = grpc.NewServer()
	s.DiscoveryServer.Register(sa.localXDSGenerator.grpcServer)
	reflection.Register(sa.localXDSGenerator.grpcServer)
}

// startXDSGenerator will start the XDS proxy and upstream. Will connect to Istiod (or XDS server),
// and start fetching configs to be cached.
// If 'RequireCerts' is set, will attempt to get certificates. Will then attempt to connect to
// the XDS server (istiod), and fetch the initial config. Once the config is ready, will start the
// local XDS proxy and return.
func (sa *Agent) startXDSGenerator(proxyConfig *meshconfig.ProxyConfig, secrets security.SecretManager, namespace string) error {
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
		XDSSAN:                   discHost,
		ResponseHandler:          sa.localXDSGenerator.proxyGen,
		XDSRootCAFile:            sa.FindRootCAForXDS(),
		RootCert:                 sa.RootCert,
		GrpcOpts:                 sa.cfg.GrpcOptions,
		Namespace:                namespace,
		InitialDiscoveryRequests: append(adsc.ConfigInitialRequests(), adsc.XdsInitialRequests()...),
	}

	// Set Secrets and JWTPath if the default ControlPlaneAuthPolicy is MUTUAL_TLS
	if sa.proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
		cfg.SecretManager = secrets
		cfg.JWTPath = sa.secOpts.JWTPath
	}

	ads, err := adsc.New(proxyConfig.DiscoveryAddress, cfg)
	if err != nil {
		// Error to be handled by caller - probably by exit if
		// we are in 'envoy using proxy' mode.
		return fmt.Errorf("adsc: %v", err)
	}
	ads.LocalCacheDir = savePath.Get()
	ads.Store = sa.localXDSGenerator.xdsServer.MemoryConfigStore
	ads.Registry = sa.localXDSGenerator.xdsServer.DiscoveryServer.MemRegistry
	if err := ads.Run(); err != nil {
		return fmt.Errorf("ADSC: failed running %v", err)
	}

	sa.localXDSGenerator.proxyGen.AddClient(ads)

	syncOk := ads.WaitConfigSync(10 * time.Second)
	if !syncOk {
		// TODO: have the store return a sync map, or better expose sync events/status
		log.Warna("Incomplete sync")
	}

	// TODO: wait for config to sync before starting the rest
	// TODO: handle push

	if sa.localXDSGenerator.listener != nil {
		go func() {
			if err := sa.localXDSGenerator.grpcServer.Serve(sa.localXDSGenerator.listener); err != nil {
				log.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
			}
		}()
	}
	return nil
}

func (sa *Agent) closeLocalXDSGenerator() {
	if sa.localXDSGenerator == nil {
		return
	}
	if sa.localXDSGenerator.grpcServer != nil {
		sa.localXDSGenerator.grpcServer.Stop()
	}
	_ = sa.localXDSGenerator.listener.Close()
	sa.localXDSGenerator.proxyGen.Close()
}
