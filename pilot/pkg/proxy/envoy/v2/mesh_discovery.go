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
	"net/http"
	"strings"
	"sync"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/api"
	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"

	"istio.io/istio/pkg/log"
)

const (
	responseTickDuration = time.Second * 15
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
	mu                sync.Mutex
	mesh              MeshDiscovery
	grpcServer        *grpc.Server
	httpServerHandler http.Handler
	pendingStreams    map[grpc.Stream]map[string]chan bool
}

// ChainHandlers adds gRPC handling to DiscoveryServer for gRPC handling while defers HTTP2 handling to the supplied http server's original handler.
func (s *DiscoveryServer) ChainHandlers(httpServer *http.Server) {
	// TODO currently only supports EDS. CDS and other gRPC services need to be plugged-in here.
	xdsapi.RegisterEndpointDiscoveryServiceServer(s.grpcServer, s)
	s.httpServerHandler = httpServer.Handler
	httpServer.Handler = s
}

// ServeHTTP implements http.Handler.
// gRPC requests are forwarded to DiscoveryServer's gRPC server and all other requests are forwarded to the original handler obtained via ChainHandler().
func (s *DiscoveryServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.ProtoMajor == 2 && strings.HasPrefix(
		r.Header.Get("Content-Type"), "application/grpc") {
		s.grpcServer.ServeHTTP(w, r)
	} else {
		s.httpServerHandler.ServeHTTP(w, r)
	}
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
	out := &DiscoveryServer{mesh: mesh, grpcServer: grpcServer, pendingStreams: make(map[grpc.Stream]map[string]chan bool)}
	return out
}

/***************************  Mesh EDS Implementation **********************************/

// StreamEndpoints implements xdsapi.EndpointDiscoveryServiceServer.StreamEndpoints().
func (s *DiscoveryServer) StreamEndpoints(stream xdsapi.EndpointDiscoveryService_StreamEndpointsServer) error {
	streamMu := &sync.Mutex{}
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// Given that Pilot holds an eventually consistent data model, Pilot ignores any acknowledgements
		// from Envoy, whether they indicate ack success or ack failure of Pilot's previous responses.
		if len(req.GetVersionInfo()) > 0 || len(req.GetResponseNonce()) > 0 {
			// This is an Envoy Ack for the previous response. We ignore this request entirely.
			continue
		}
		clusters := req.GetResourceNames()
		reqKey := fmt.Sprintf("StreamEndpoints|%v", clusters)
		if log.InfoEnabled() {
			log.Infof("Stream requested for %q", reqKey)
		}
		chanLoopDone := s.newResponseLoop(stream, reqKey)
		// Periodically send the ClusterLoadAssignment to the stream peer until this stream is closed or the timer is
		// closed in response to a new request with the same request key.
		go func(clusters []string, stream xdsapi.EndpointDiscoveryService_StreamEndpointsServer, streamMu *sync.Mutex,
			reqKey string, chanLoopDone chan bool) {
			defer s.stopResponseLoop(stream, reqKey, chanLoopDone)
			for {
				select {
				case <-chanLoopDone:
					return
				default:
					streamMu.Lock()
					err := stream.Send(s.mesh.Endpoints(clusters))
					streamMu.Unlock()
					if err != nil {
						break
					}
					log.Infof("Stream response sent for request %q", reqKey)
					time.Sleep(responseTickDuration)
				}
			}
		}(clusters, stream, streamMu, reqKey, chanLoopDone)
	}
}

// FetchEndpoints implements xdsapi.EndpointDiscoveryServiceServer.FetchEndpoints().
func (s *DiscoveryServer) FetchEndpoints(ctx context.Context, req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	clusters := req.GetResourceNames()
	if log.InfoEnabled() {
		log.Infof("Fetch requested for %v", clusters)
	}
	return s.mesh.Endpoints(clusters), nil
}

// StreamLoadStats implements xdsapi.EndpointDiscoveryServiceServer.StreamLoadStats().
func (s *DiscoveryServer) StreamLoadStats(xdsapi.EndpointDiscoveryService_StreamLoadStatsServer) error {
	// TODO: Change fake values to real load assignments
	return errors.New("unsupported streaming method")
}

/***************************  Common logic used by most  **********************************
****************************    gRPC streaming methods   **********************************/

func (s *DiscoveryServer) newResponseLoop(stream grpc.Stream, requestKey string) chan bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	resMap, found := s.pendingStreams[stream]
	if !found {
		resMap = map[string]chan bool{}
		s.pendingStreams[stream] = resMap
	} else {
		respLoopDone, found := resMap[requestKey]
		if found {
			respLoopDone <- true
		}
	}
	chanLoopDone := make(chan bool, 1)
	resMap[requestKey] = chanLoopDone
	return chanLoopDone
}

func (s *DiscoveryServer) stopResponseLoop(stream grpc.Stream, requestKey string, chanLoopDone chan bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resMap, found := s.pendingStreams[stream]
	if !found {
		return
	}
	prevChanLoopDone, found := resMap[requestKey]
	if found {
		if chanLoopDone != prevChanLoopDone {
			return
		}
		close(chanLoopDone)
		delete(resMap, requestKey)
		if len(resMap) == 0 {
			delete(s.pendingStreams, stream)
		}
	}
}
