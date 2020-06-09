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
	"context"
	"net"
	"time"

	"google.golang.org/grpc"
	"istio.io/istio/security/pkg/nodeagent/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
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

func initXDS(mc *meshconfig.ProxyConfig) {
	s := xds.NewXDS()
	xdsServer = s
	cfg = mc

	// GrpcServer server over UDS, shared by SDS and XDS
	serverOptions.GrpcServer = grpc.NewServer()

	var err error
	if xdsAddr.Get() != "" {
		localListener, err = net.Listen("tcp", xdsAddr.Get())
		if err != nil {
			log.Errorf("Failed to set up TCP path: %v", err)
		}
		localGrpcServer = grpc.NewServer()
		s.DiscoveryServer.Register(localGrpcServer)
	}
}

func startXDS(proxyConfig *meshconfig.ProxyConfig, secrets *cache.SecretCache) error {
	// TODO: handle certificates and security - similar with the
	// code we generate for envoy !

	ads, err := adsc.Dial(proxyConfig.DiscoveryAddress,
		"",
		&adsc.Config{
			Meta: model.NodeMetadata{
				Generator: "api",
			}.ToStruct(),
			Secrets: secrets,
		})
	if err != nil {
		log.Fatalf("Failed to connect to XDS server")
	}

	ads.LocalCacheDir = savePath.Get()
	ads.Store = xdsServer.MemoryConfigStore
	ads.Registry = xdsServer.DiscoveryServer.MemRegistry

	ads.WatchConfig()
	syncOk := xdsServer.WaitConfigSync(10 * time.Second)
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
