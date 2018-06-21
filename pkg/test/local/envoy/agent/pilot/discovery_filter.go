//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package pilot

import (
	"net"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/local/envoy/discovery"
)

const (
	maxStreams   = 100000
	listenerType = "type.googleapis.com/envoy.api.v2.Listener"
)

// discoveryFilter manages a connection to Pilot an applies a filter to received discovery responses.
type discoveryFilter struct {
	grpcServer *grpc.Server
	proxy      *discovery.Filter
	proxyAddr  *net.TCPAddr
}

func (f *discoveryFilter) start(discoveryAddr string) (err error) {
	f.proxy = &discovery.Filter{
		DiscoveryAddr: discoveryAddr,
		FilterFunc:    f.filterDiscoveryResponse,
	}

	// Start a GRPC server and register the proxy handlers.
	f.grpcServer = grpc.NewServer(grpc.MaxConcurrentStreams(uint32(maxStreams)))
	// get the grpc server wired up
	grpc.EnableTracing = true
	f.proxy.Register(f.grpcServer)

	// Dynamically assign a port for the proxy's GRPC server.
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}

	f.proxyAddr = listener.Addr().(*net.TCPAddr)

	go func() {
		if err = f.grpcServer.Serve(listener); err != nil {
			log.Warna(err)
		}
	}()
	return nil
}

func (f *discoveryFilter) filterDiscoveryResponse(resp *xdsapi.DiscoveryResponse) (*xdsapi.DiscoveryResponse, error) {
	newResponse := xdsapi.DiscoveryResponse{
		TypeUrl:     resp.TypeUrl,
		Canary:      resp.Canary,
		VersionInfo: resp.VersionInfo,
		Nonce:       resp.Nonce,
	}

	// TODO(nmittler): Remove management clusters (port 3333, 9999)
	// TODO(nmittler): Make inbound listeners for external services bound to a port.
	for _, any := range resp.Resources {
		switch any.TypeUrl {
		case listenerType:
			l := &xdsapi.Listener{}
			if err := l.Unmarshal(any.Value); err != nil {
				return nil, err
			}
			switch l.Name {
			case "virtual":
				// Exclude the iptables-mapped listener from the Envoy configuration. It's hard-coded to port 15001,
				// which will likely fail to be bound.
				continue
			default:
				newResponse.Resources = append(newResponse.Resources, any)
			}
		default:
			newResponse.Resources = append(newResponse.Resources, any)
		}
	}

	return &newResponse, nil
}

func (f *discoveryFilter) stop() {
	f.grpcServer.Stop()
}
