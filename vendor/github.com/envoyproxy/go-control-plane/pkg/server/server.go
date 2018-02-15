// Copyright 2017 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

// Package server provides an implementation of a streaming xDS server.
package server

import (
	"context"
	"strconv"
	"sync/atomic"

	"github.com/envoyproxy/go-control-plane/api"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Resource types in xDS v2.
const (
	typePrefix   = "type.googleapis.com/envoy.api.v2."
	EndpointType = typePrefix + "ClusterLoadAssignment"
	ClusterType  = typePrefix + "Cluster"
	RouteType    = typePrefix + "RouteConfiguration"
	ListenerType = typePrefix + "Listener"
	AnyType      = ""
)

// GetTypeURL retrieves type URL by response type.
func GetTypeURL(typ cache.ResponseType) string {
	switch typ {
	case cache.EndpointResponse:
		return EndpointType
	case cache.ClusterResponse:
		return ClusterType
	case cache.RouteResponse:
		return RouteType
	case cache.ListenerResponse:
		return ListenerType
	}
	return AnyType
}

// Server is a collection of handlers for streaming discovery requests.
type Server interface {
	api.AggregatedDiscoveryServiceServer
	api.EndpointDiscoveryServiceServer
	api.ClusterDiscoveryServiceServer
	api.RouteDiscoveryServiceServer
	api.ListenerDiscoveryServiceServer
}

// NewServer creates handlers from a config watcher.
func NewServer(config cache.ConfigWatcher) Server {
	return &server{config: config}
}

type server struct {
	config cache.ConfigWatcher

	// streamCount for counting bi-di streams
	streamCount int64
}

type stream interface {
	Send(*api.DiscoveryResponse) error
	Recv() (*api.DiscoveryRequest, error)
}

// watches for all xDS resource types
type watches struct {
	endpoints cache.Watch
	clusters  cache.Watch
	routes    cache.Watch
	listeners cache.Watch

	endpointNonce string
	clusterNonce  string
	routeNonce    string
	listenerNonce string
}

// cancel all watches
func (values watches) cancel() {
	values.endpoints.Cancel()
	values.clusters.Cancel()
	values.routes.Cancel()
	values.listeners.Cancel()
}

// process handles a bi-di stream request
func (s *server) process(stream stream, reqCh <-chan *api.DiscoveryRequest, defaultTypeURL string) error {
	// increment stream count
	streamID := atomic.AddInt64(&s.streamCount, 1)

	// unique nonce generator for req-resp pairs per xDS stream; the server
	// ignores stale nonces. nonce is only modified within send() function.
	var streamNonce int64

	// a collection of watches per request type
	var values watches
	defer func() {
		values.cancel()
	}()

	// sends a response by serializing to protobuf Any
	send := func(resp cache.Response, typeURL string) (string, error) {
		resources := make([]*types.Any, len(resp.Resources))
		streamNonce = streamNonce + 1
		for i := 0; i < len(resp.Resources); i++ {
			data, err := proto.Marshal(resp.Resources[i])
			if err != nil {
				return "", err
			}
			resources[i] = &types.Any{
				TypeUrl: typeURL,
				Value:   data,
			}
		}
		nonce := strconv.FormatInt(streamNonce, 10)
		glog.V(10).Infof("[%d] respond %s with nonce %q version %q", streamID, typeURL, nonce, resp.Version)
		out := &api.DiscoveryResponse{
			VersionInfo: resp.Version,
			Resources:   resources,
			Canary:      resp.Canary,
			TypeUrl:     typeURL,
			Nonce:       nonce,
		}
		return nonce, stream.Send(out)
	}

	glog.V(10).Infof("[%d] open stream for %q", streamID, defaultTypeURL)
	for {
		select {
		// config watcher can send the requested resources types in any order
		case resp, more := <-values.endpoints.Value:
			if !more {
				return status.Errorf(codes.Unavailable, "endpoints watch failed")
			}
			nonce, err := send(resp, EndpointType)
			if err != nil {
				return err
			}
			values.endpointNonce = nonce

		case resp, more := <-values.clusters.Value:
			if !more {
				return status.Errorf(codes.Unavailable, "clusters watch failed")
			}
			nonce, err := send(resp, ClusterType)
			if err != nil {
				return err
			}
			values.clusterNonce = nonce

		case resp, more := <-values.routes.Value:
			if !more {
				return status.Errorf(codes.Unavailable, "routes watch failed")
			}
			nonce, err := send(resp, RouteType)
			if err != nil {
				return err
			}
			values.routeNonce = nonce

		case resp, more := <-values.listeners.Value:
			if !more {
				return status.Errorf(codes.Unavailable, "listeners watch failed")
			}
			nonce, err := send(resp, ListenerType)
			if err != nil {
				return err
			}
			values.listenerNonce = nonce

		case req, more := <-reqCh:
			// input stream ended or errored out
			if !more {
				glog.V(10).Infof("[%d] stream closed", streamID)
				return nil
			}

			// nonces can be reused across streams; we verify nonce only if nonce is not initialized
			nonce := req.GetResponseNonce()

			// type URL is required for ADS but is implicit for xDS
			typeURL := req.TypeUrl
			if defaultTypeURL == AnyType {
				if typeURL == "" {
					return status.Errorf(codes.InvalidArgument, "type URL is required for ADS")
				}
			} else if typeURL == "" {
				typeURL = defaultTypeURL
			}

			glog.V(10).Infof("[%d] request %s%v with nonce %q from version %q", streamID, typeURL,
				req.GetResourceNames(), nonce, req.GetVersionInfo())

			// cancel existing watches to (re-)request a newer version
			switch {
			case typeURL == EndpointType && (values.endpointNonce == "" || values.endpointNonce == nonce):
				values.endpoints.Cancel()
				values.endpoints = s.config.Watch(cache.EndpointResponse, req.GetNode(), req.GetVersionInfo(), req.GetResourceNames())
			case typeURL == ClusterType && (values.clusterNonce == "" || values.clusterNonce == nonce):
				values.clusters.Cancel()
				values.clusters = s.config.Watch(cache.ClusterResponse, req.GetNode(), req.GetVersionInfo(), req.GetResourceNames())
			case typeURL == RouteType && (values.routeNonce == "" || values.routeNonce == nonce):
				values.routes.Cancel()
				values.routes = s.config.Watch(cache.RouteResponse, req.GetNode(), req.GetVersionInfo(), req.GetResourceNames())
			case typeURL == ListenerType && (values.listenerNonce == "" || values.listenerNonce == nonce):
				values.listeners.Cancel()
				values.listeners = s.config.Watch(cache.ListenerResponse, req.GetNode(), req.GetVersionInfo(), req.GetResourceNames())
			}
		}
	}
}

// handler converts a blocking read call to channels and initiates stream processing
func (s *server) handler(stream stream, typeURL string) error {
	// a channel for receiving incoming requests
	reqCh := make(chan *api.DiscoveryRequest)
	reqStop := int32(0)
	go func() {
		for {
			req, err := stream.Recv()
			if atomic.LoadInt32(&reqStop) != 0 {
				return
			}
			if err != nil {
				close(reqCh)
				return
			}
			reqCh <- req
		}
	}()

	err := s.process(stream, reqCh, typeURL)

	// prevents writing to a closed channel if send failed on blocked recv
	// TODO(kuat) figure out how to unblock recv through gRPC API
	atomic.StoreInt32(&reqStop, 1)

	return err
}

func (s *server) StreamAggregatedResources(stream api.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	return s.handler(stream, AnyType)
}

func (s *server) StreamEndpoints(stream api.EndpointDiscoveryService_StreamEndpointsServer) error {
	return s.handler(stream, EndpointType)
}

func (s *server) StreamLoadStats(stream api.EndpointDiscoveryService_StreamLoadStatsServer) error {
	return status.Errorf(codes.Unimplemented, "not implemented")
}

func (s *server) StreamClusters(stream api.ClusterDiscoveryService_StreamClustersServer) error {
	return s.handler(stream, ClusterType)
}

func (s *server) StreamRoutes(stream api.RouteDiscoveryService_StreamRoutesServer) error {
	return s.handler(stream, RouteType)
}

func (s *server) StreamListeners(stream api.ListenerDiscoveryService_StreamListenersServer) error {
	return s.handler(stream, ListenerType)
}

func (s *server) FetchEndpoints(ctx context.Context, req *api.DiscoveryRequest) (*api.DiscoveryResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not implemented")
}

func (s *server) FetchClusters(ctx context.Context, req *api.DiscoveryRequest) (*api.DiscoveryResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not implemented")
}

func (s *server) FetchRoutes(ctx context.Context, req *api.DiscoveryRequest) (*api.DiscoveryResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not implemented")
}

func (s *server) FetchListeners(ctx context.Context, req *api.DiscoveryRequest) (*api.DiscoveryResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not implemented")
}
