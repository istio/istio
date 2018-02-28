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

package v2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pkg/log"
)

const (
	responseTickDuration = time.Second * 15
)

var (
	gRPCPort string
)

// MeshDiscovery is a unified interface for Envoy's v2 xDS APIs and Pilot's older
// data structure model.
// For Envoy terminology: https://www.envoyproxy.io/docs/envoy/latest/api-v2/api
// For Pilot older data structure model: istio.io/pilot/pkg/model
//
// Implementations of MeshDiscovery are required to be threadsafe.
type MeshDiscovery interface {
	// Endpoints implements EDS and returns a list of endpoints by subset for the list of supplied subsets.
	// In Envoy's terminology a subset is service cluster.
	Endpoints(serviceClusters []string) *xdsapi.DiscoveryResponse
}

// DiscoveryServer is Pilot's gRPC implementation for Envoy's v2 xds APIs
type DiscoveryServer struct {
	// mesh holds the reference to Pilot's internal data structures that provide mesh discovery capability.
	mesh MeshDiscovery
	// grpcServer supports gRPC for xDS v2 services.
	grpcServer *grpc.Server
}

func init() {
	// TODO: move this to Pilot's configuration.
	tempGRPCPort := os.Getenv("PILOT_GRPC_PORT")
	portNum, err := strconv.Atoi(tempGRPCPort)
	if err == nil && portNum > 1000 {
		gRPCPort = tempGRPCPort
	}
}

// Enabled returns true if EnvoyV2 APIs are enabled for Pilot.
func Enabled() bool {
	return len(gRPCPort) > 0
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(mesh MeshDiscovery) *DiscoveryServer {
	// TODO for now use hard coded / default gRPC options. The constructor may evolve to use interfaces that guide specific options later.
	// Example:
	//		grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(uint32(someconfig.MaxConcurrentStreams)))
	var grpcOptions []grpc.ServerOption

	var interceptors []grpc.UnaryServerInterceptor

	// TODO: log request interceptor if debug enabled.

	// setup server prometheus monitoring (as final interceptor in chain)
	interceptors = append(interceptors, grpc_prometheus.UnaryServerInterceptor)
	grpc_prometheus.EnableHandlingTimeHistogram()

	grpcOptions = append(grpcOptions, grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(interceptors...)))

	// get the grpc server wired up
	grpc.EnableTracing = true

	grpcServer := grpc.NewServer(grpcOptions...)
	out := &DiscoveryServer{mesh: mesh, grpcServer: grpcServer}
	xdsapi.RegisterEndpointDiscoveryServiceServer(out.grpcServer, out)
	return out
}

// Start opens up a listening port for serving gRPC requests and starts up the underlying gRPC server.
// This method is non-blocking.
func (s *DiscoveryServer) Start() {
	lis, err := net.Listen("tcp", ":"+gRPCPort)
	if err != nil {
		log.Errorf("mesh discovery server failed to listen on port %q: %v", gRPCPort, err)
		return
	}
	// Serve is a blocking call.
	go func(gRPCServer *grpc.Server, lis net.Listener) {
		log.Infof("attempting to start mesh discovery server on port %q", gRPCPort)
		if err := gRPCServer.Serve(lis); err != nil {
			log.Errorf("mesh discovery server failed to serve on port %q: %v", gRPCPort, err)
		}
	}(s.grpcServer, lis)
}

// Stop performs a hard shutsdown the underlying gRPC server.
// TODO: We may need to make a distinction between gracefully shutting down Pilot vs hard shutdown
func (s *DiscoveryServer) Stop() {
	s.grpcServer.Stop()
}

/***************************  Mesh EDS Implementation **********************************/

// StreamEndpoints implements xdsapi.EndpointDiscoveryServiceServer.StreamEndpoints().
func (s *DiscoveryServer) StreamEndpoints(stream xdsapi.EndpointDiscoveryService_StreamEndpointsServer) error {
	ticker := time.NewTicker(responseTickDuration)
	peerInfo, ok := peer.FromContext(stream.Context())
	peerAddr := "Unknown peer address"
	if ok {
		peerAddr = peerInfo.Addr.String()
	}
	defer ticker.Stop()
	var discReq *xdsapi.DiscoveryRequest
	var receiveError error
	var clusters []string
	initialRequest := true
	reqChannel := make(chan *xdsapi.DiscoveryRequest, 1)
	go func() {
		defer close(reqChannel)
		for {
			req, err := stream.Recv()
			if err != nil {
				if status.Code(err) == codes.Canceled || err == io.EOF {
					return
				}
				receiveError = err
				log.Errorf("request loop for EDS for client %q terminated with errors %v", peerAddr, err)
				return
			}
			reqChannel <- req
		}
	}()
	for {
		// Block until either a request is received or the ticker ticks
		select {
		case discReq, ok = <-reqChannel:
			if !ok {
				return receiveError
			}
			if !initialRequest {
				// Given that Pilot holds an eventually consistent data model, Pilot ignores any acknowledgements
				// from Envoy, whether they indicate ack success or ack failure of Pilot's previous responses.
				// Only the first request is actually horored and processed. Pilot drains all other requests from this stream.
				if log.DebugEnabled() {
					log.Debugf("EDS ACK from Envoy for client %q has version %q and Nonce %q for request for clusters %v",
						discReq.GetVersionInfo(), discReq.GetResponseNonce(), clusters)
				}
				continue
			}
		case <-ticker.C:
			if !initialRequest {
				// Ignore ticker events until the very first request is processed.
				continue
			}
		}
		if initialRequest {
			initialRequest = false
			clusters = discReq.GetResourceNames()
			if len(clusters) == 0 {
				return fmt.Errorf("no clusters specified in EDS request from %q", peerAddr)
			}
			if log.DebugEnabled() {
				log.Debugf("EDS request from  %q for clusters %v received.", peerAddr, clusters)
			}
		}
		response := s.mesh.Endpoints(clusters)
		err := stream.Send(response)
		if err != nil {
			return err
		}
		if log.DebugEnabled() {
			log.Debugf("\nEDS response from  %q for clusters %v, Response: \n%s\n\n", peerAddr, clusters, response.String())
		}
	}
}

// FetchEndpoints implements xdsapi.EndpointDiscoveryServiceServer.FetchEndpoints().
func (s *DiscoveryServer) FetchEndpoints(ctx context.Context, req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	peerInfo, ok := peer.FromContext(ctx)
	peerAddr := "Unknown peer address"
	if ok {
		peerAddr = peerInfo.Addr.String()
	}
	clusters := req.GetResourceNames()
	if len(clusters) == 0 {
		return nil, fmt.Errorf("no clusters specified in EDS request from %q", peerAddr)
	}
	if log.DebugEnabled() {
		log.Debugf("EDS request from  %q for clusters %v received.", peerAddr, clusters)
	}
	response := s.mesh.Endpoints(clusters)
	if log.DebugEnabled() {
		log.Debugf("\nEDS response from  %q for clusters %v, Response: \n%s\n\n", peerAddr, clusters, response.String())
	}
	return response, nil
}

// StreamLoadStats implements xdsapi.EndpointDiscoveryServiceServer.StreamLoadStats().
func (s *DiscoveryServer) StreamLoadStats(xdsapi.EndpointDiscoveryService_StreamEndpointsServer) error {
	// TODO: Change fake values to real load assignments
	return errors.New("unsupported streaming method")
}
