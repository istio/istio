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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/xds"
	"istio.io/istio/pkg/adsc"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

// xds-agent runs an XDS server in agent, similar with the SDS server. It is using the same
// UDS socket as the SDS server - envoy will use a single connection/cluster for both.
//
// Envoy will connect to the agent using UDS. Agent will handle connection to one
// or more upstream XDS servers, and return aggregated and possibly patched results.

// This is part of an optimization experiment and exploring federation and local
// transformations - not enabled by default and not intended for production use yet.

var (
	udsListener net.Listener

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
		"Address for a local XDS proxy.")

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
	udsListener, err = setUpUds(serverOptions.WorkloadUDSPath)
	if err != nil {
		log.Errorf("Failed to set up UDS path: %v", err)
	}

	if xdsAddr.Get() != "" {
		localListener, err = net.Listen("tcp", xdsAddr.Get())
		if err != nil {
			log.Errorf("Failed to set up TCP path: %v", err)
		}
		localGrpcServer = grpc.NewServer()
		s.DiscoveryServer.Register(localGrpcServer)
	}
}

func startXDSClient(addr string) {
	// TODO: handle certificates and security !
	ads, err := adsc.Dial(addr, "", &adsc.Config{
		Meta: model.NodeMetadata{
			Generator: "api",
		}.ToStruct(),
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

	if udsListener != nil {
		go func() {
			if err := serverOptions.GrpcServer.Serve(udsListener); err != nil {
				log.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
			}
		}()
	}
	if localListener != nil {
		go func() {
			if err := localGrpcServer.Serve(localListener); err != nil {
				log.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
			}
		}()
	}
}

func startXDS() {
	startXDSClient(cfg.DiscoveryAddress)

	// TODO: wait for config to sync before starting the rest
	// TODO: handle push

	if udsListener != nil {
		go func() {
			if err := serverOptions.GrpcServer.Serve(udsListener); err != nil {
				log.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
			}
		}()
	}
	if localListener != nil {
		go func() {
			if err := localGrpcServer.Serve(localListener); err != nil {
				log.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
			}
		}()
	}
}

// copied from sds agent - will be removed from there in next cleanup.
func setUpUds(udsPath string) (net.Listener, error) {
	// Remove unix socket before use.
	if err := os.Remove(udsPath); err != nil && !os.IsNotExist(err) {
		// Anything other than "file not found" is an error.
		log.Errorf("Failed to remove unix://%s: %v", udsPath, err)
		return nil, fmt.Errorf("failed to remove unix://%s", udsPath)
	}

	// Attempt to create the folder in case it doesn't exist
	if err := os.MkdirAll(filepath.Dir(udsPath), 0750); err != nil {
		// If we cannot create it, just warn here - we will fail later if there is a real error
		log.Warnf("Failed to create directory for %v: %v", udsPath, err)
	}

	var err error
	udsListener, err := net.Listen("unix", udsPath)
	if err != nil {
		log.Errorf("Failed to listen on unix socket %q: %v", udsPath, err)
		return nil, err
	}

	// Update SDS UDS file permission so that istio-proxy has permission to access it.
	if _, err := os.Stat(udsPath); err != nil {
		log.Errorf("SDS uds file %q doesn't exist", udsPath)
		return nil, fmt.Errorf("sds uds file %q doesn't exist", udsPath)
	}
	if err := os.Chmod(udsPath, 0666); err != nil {
		log.Errorf("Failed to update %q permission", udsPath)
		return nil, fmt.Errorf("failed to update %q permission", udsPath)
	}

	return udsListener, nil
}
