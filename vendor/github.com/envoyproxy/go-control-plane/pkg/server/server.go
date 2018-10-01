// Copyright 2018 Envoyproxy Authors
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
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
)

// Server is a collection of handlers for streaming discovery requests.
type Server interface {
	v2.EndpointDiscoveryServiceServer
	v2.ClusterDiscoveryServiceServer
	v2.RouteDiscoveryServiceServer
	v2.ListenerDiscoveryServiceServer
	discovery.AggregatedDiscoveryServiceServer

	// Fetch is the universal fetch method.
	Fetch(context.Context, *v2.DiscoveryRequest) (*v2.DiscoveryResponse, error)
}

// Callbacks is a collection of callbacks inserted into the server operation.
// The callbacks are invoked synchronously.
type Callbacks interface {
	// OnStreamOpen is called once an xDS stream is open with a stream ID and the type URL (or "" for ADS).
	// Returning an error will end processing and close the stream. OnStreamClosed will still be called.
	OnStreamOpen(context.Context, int64, string) error
	// OnStreamClosed is called immediately prior to closing an xDS stream with a stream ID.
	OnStreamClosed(int64)
	// OnStreamRequest is called once a request is received on a stream.
	OnStreamRequest(int64, *v2.DiscoveryRequest)
	// OnStreamResponse is called immediately prior to sending a response on a stream.
	OnStreamResponse(int64, *v2.DiscoveryRequest, *v2.DiscoveryResponse)
	// OnFetchRequest is called for each Fetch request. Returning an error will end processing of the
	// request and respond with an error.
	OnFetchRequest(context.Context, *v2.DiscoveryRequest) error
	// OnFetchResponse is called immediately prior to sending a response.
	OnFetchResponse(*v2.DiscoveryRequest, *v2.DiscoveryResponse)
}

// NewServer creates handlers from a config watcher and callbacks.
func NewServer(config cache.Cache, callbacks Callbacks) Server {
	return &server{cache: config, callbacks: callbacks}
}

type server struct {
	cache     cache.Cache
	callbacks Callbacks

	// streamCount for counting bi-di streams
	streamCount int64
}

type stream interface {
	grpc.ServerStream

	Send(*v2.DiscoveryResponse) error
	Recv() (*v2.DiscoveryRequest, error)
}

// watches for all xDS resource types
type watches struct {
	endpoints chan cache.Response
	clusters  chan cache.Response
	routes    chan cache.Response
	listeners chan cache.Response

	endpointCancel func()
	clusterCancel  func()
	routeCancel    func()
	listenerCancel func()

	endpointNonce string
	clusterNonce  string
	routeNonce    string
	listenerNonce string
}

// Cancel all watches
func (values watches) Cancel() {
	if values.endpointCancel != nil {
		values.endpointCancel()
	}
	if values.clusterCancel != nil {
		values.clusterCancel()
	}
	if values.routeCancel != nil {
		values.routeCancel()
	}
	if values.listenerCancel != nil {
		values.listenerCancel()
	}
}

func createResponse(resp *cache.Response, typeURL string) (*v2.DiscoveryResponse, error) {
	if resp == nil {
		return nil, errors.New("missing response")
	}
	resources := make([]types.Any, len(resp.Resources))
	for i := 0; i < len(resp.Resources); i++ {
		data, err := proto.Marshal(resp.Resources[i])
		if err != nil {
			return nil, err
		}
		resources[i] = types.Any{
			TypeUrl: typeURL,
			Value:   data,
		}
	}
	out := &v2.DiscoveryResponse{
		VersionInfo: resp.Version,
		Resources:   resources,
		TypeUrl:     typeURL,
	}
	return out, nil
}

// process handles a bi-di stream request
func (s *server) process(stream stream, reqCh <-chan *v2.DiscoveryRequest, defaultTypeURL string) error {
	// increment stream count
	streamID := atomic.AddInt64(&s.streamCount, 1)

	// unique nonce generator for req-resp pairs per xDS stream; the server
	// ignores stale nonces. nonce is only modified within send() function.
	var streamNonce int64

	// a collection of watches per request type
	var values watches
	defer func() {
		values.Cancel()
		if s.callbacks != nil {
			s.callbacks.OnStreamClosed(streamID)
		}
	}()

	// sends a response by serializing to protobuf Any
	send := func(resp cache.Response, typeURL string) (string, error) {
		out, err := createResponse(&resp, typeURL)
		if err != nil {
			return "", err
		}

		// increment nonce
		streamNonce = streamNonce + 1
		out.Nonce = strconv.FormatInt(streamNonce, 10)
		if s.callbacks != nil {
			s.callbacks.OnStreamResponse(streamID, &resp.Request, out)
		}
		return out.Nonce, stream.Send(out)
	}

	if s.callbacks != nil {
		if err := s.callbacks.OnStreamOpen(stream.Context(), streamID, defaultTypeURL); err != nil {
			return err
		}
	}

	for {
		select {
		// config watcher can send the requested resources types in any order
		case resp, more := <-values.endpoints:
			if !more {
				return status.Errorf(codes.Unavailable, "endpoints watch failed")
			}
			nonce, err := send(resp, cache.EndpointType)
			if err != nil {
				return err
			}
			values.endpointNonce = nonce

		case resp, more := <-values.clusters:
			if !more {
				return status.Errorf(codes.Unavailable, "clusters watch failed")
			}
			nonce, err := send(resp, cache.ClusterType)
			if err != nil {
				return err
			}
			values.clusterNonce = nonce

		case resp, more := <-values.routes:
			if !more {
				return status.Errorf(codes.Unavailable, "routes watch failed")
			}
			nonce, err := send(resp, cache.RouteType)
			if err != nil {
				return err
			}
			values.routeNonce = nonce

		case resp, more := <-values.listeners:
			if !more {
				return status.Errorf(codes.Unavailable, "listeners watch failed")
			}
			nonce, err := send(resp, cache.ListenerType)
			if err != nil {
				return err
			}
			values.listenerNonce = nonce

		case req, more := <-reqCh:
			// input stream ended or errored out
			if !more {
				return nil
			}
			if req == nil {
				return status.Errorf(codes.Unavailable, "empty request")
			}

			// nonces can be reused across streams; we verify nonce only if nonce is not initialized
			nonce := req.GetResponseNonce()

			// type URL is required for ADS but is implicit for xDS
			if defaultTypeURL == cache.AnyType {
				if req.TypeUrl == "" {
					return status.Errorf(codes.InvalidArgument, "type URL is required for ADS")
				}
			} else if req.TypeUrl == "" {
				req.TypeUrl = defaultTypeURL
			}

			if s.callbacks != nil {
				s.callbacks.OnStreamRequest(streamID, req)
			}

			// cancel existing watches to (re-)request a newer version
			switch {
			case req.TypeUrl == cache.EndpointType && (values.endpointNonce == "" || values.endpointNonce == nonce):
				if values.endpointCancel != nil {
					values.endpointCancel()
				}
				values.endpoints, values.endpointCancel = s.cache.CreateWatch(*req)
			case req.TypeUrl == cache.ClusterType && (values.clusterNonce == "" || values.clusterNonce == nonce):
				if values.clusterCancel != nil {
					values.clusterCancel()
				}
				values.clusters, values.clusterCancel = s.cache.CreateWatch(*req)
			case req.TypeUrl == cache.RouteType && (values.routeNonce == "" || values.routeNonce == nonce):
				if values.routeCancel != nil {
					values.routeCancel()
				}
				values.routes, values.routeCancel = s.cache.CreateWatch(*req)
			case req.TypeUrl == cache.ListenerType && (values.listenerNonce == "" || values.listenerNonce == nonce):
				if values.listenerCancel != nil {
					values.listenerCancel()
				}
				values.listeners, values.listenerCancel = s.cache.CreateWatch(*req)
			}
		}
	}
}

// handler converts a blocking read call to channels and initiates stream processing
func (s *server) handler(stream stream, typeURL string) error {
	// a channel for receiving incoming requests
	reqCh := make(chan *v2.DiscoveryRequest)
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

func (s *server) StreamAggregatedResources(stream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	return s.handler(stream, cache.AnyType)
}

func (s *server) StreamEndpoints(stream v2.EndpointDiscoveryService_StreamEndpointsServer) error {
	return s.handler(stream, cache.EndpointType)
}

func (s *server) StreamClusters(stream v2.ClusterDiscoveryService_StreamClustersServer) error {
	return s.handler(stream, cache.ClusterType)
}

func (s *server) StreamRoutes(stream v2.RouteDiscoveryService_StreamRoutesServer) error {
	return s.handler(stream, cache.RouteType)
}

func (s *server) StreamListeners(stream v2.ListenerDiscoveryService_StreamListenersServer) error {
	return s.handler(stream, cache.ListenerType)
}

// Fetch is the universal fetch method.
func (s *server) Fetch(ctx context.Context, req *v2.DiscoveryRequest) (*v2.DiscoveryResponse, error) {
	if s.callbacks != nil {
		if err := s.callbacks.OnFetchRequest(ctx, req); err != nil {
			return nil, err
		}
	}
	resp, err := s.cache.Fetch(ctx, *req)
	if err != nil {
		return nil, err
	}
	out, err := createResponse(resp, req.TypeUrl)
	if s.callbacks != nil {
		s.callbacks.OnFetchResponse(req, out)
	}
	return out, err
}

func (s *server) FetchEndpoints(ctx context.Context, req *v2.DiscoveryRequest) (*v2.DiscoveryResponse, error) {
	if req == nil {
		return nil, status.Errorf(codes.Unavailable, "empty request")
	}
	req.TypeUrl = cache.EndpointType
	return s.Fetch(ctx, req)
}

func (s *server) FetchClusters(ctx context.Context, req *v2.DiscoveryRequest) (*v2.DiscoveryResponse, error) {
	if req == nil {
		return nil, status.Errorf(codes.Unavailable, "empty request")
	}
	req.TypeUrl = cache.ClusterType
	return s.Fetch(ctx, req)
}

func (s *server) FetchRoutes(ctx context.Context, req *v2.DiscoveryRequest) (*v2.DiscoveryResponse, error) {
	if req == nil {
		return nil, status.Errorf(codes.Unavailable, "empty request")
	}
	req.TypeUrl = cache.RouteType
	return s.Fetch(ctx, req)
}

func (s *server) FetchListeners(ctx context.Context, req *v2.DiscoveryRequest) (*v2.DiscoveryResponse, error) {
	if req == nil {
		return nil, status.Errorf(codes.Unavailable, "empty request")
	}
	req.TypeUrl = cache.ListenerType
	return s.Fetch(ctx, req)
}

func (s *server) IncrementalAggregatedResources(_ discovery.AggregatedDiscoveryService_IncrementalAggregatedResourcesServer) error {
	return errors.New("not implemented")
}

func (s *server) IncrementalClusters(_ v2.ClusterDiscoveryService_IncrementalClustersServer) error {
	return errors.New("not implemented")
}

func (s *server) IncrementalRoutes(_ v2.RouteDiscoveryService_IncrementalRoutesServer) error {
	return errors.New("not implemented")
}
