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
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/proxy/envoy/v1"

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
	// mesh holds the reference to Pilot's internal data structures that provide mesh discovery capability.
	mesh *v1.DiscoveryService
	// GrpcServer supports gRPC for xDS v2 services.
	GrpcServer *grpc.Server

	Connections map[string]*EdsConnection
}

// EdsConnection represents a streaming connection from an envoy server
type EdsConnection struct {
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(mesh *v1.DiscoveryService, grpcServer *grpc.Server) *DiscoveryServer {
	out := &DiscoveryServer{mesh: mesh, GrpcServer: grpcServer}
	xdsapi.RegisterEndpointDiscoveryServiceServer(out.GrpcServer, out)

	return out
}

/***************************  Mesh EDS Implementation **********************************/
// TODO: move to eds.go (separate PR for easier review of the changes)

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
				log.Errorf("EDS close for client %q terminated with errors %v", peerAddr, err)
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
			clusters2 := discReq.GetResourceNames()
			// TODO: is it additive ? Does override ever happens ?
			if len(clusters) > 0 && len(clusters2) > 0 {
				log.Infof("Clusters override %v -> %v %s", clusters, clusters2, discReq.String())
			}
			// Given that Pilot holds an eventually consistent data model, Pilot ignores any acknowledgements
			// from Envoy, whether they indicate ack success or ack failure of Pilot's previous responses.
			// Only the first request is actually horored and processed. Pilot drains all other requests from this stream.
			if discReq.ResponseNonce != "" {
				log.Infof("EDS ACK %s from Envoy, existing clusters %v ",
					discReq.String(), clusters)
				continue
			} else {
				if len(clusters) > 0 {
					log.Infof("EDS REQ %s from Envoy, existing clusters %v ",
						discReq.String(), clusters)
				} else {
					log.Infof("EDS REQ %s %v", discReq.String(), peerAddr)
				}
			}
			if len(clusters2) > 0 {
				clusters = clusters2
			}

		case <-ticker.C:
		}

		if len(clusters) > 0 {
			response := Endpoints(s.mesh, clusters)
			err := stream.Send(response)
			if err != nil {
				return err
			}

			log.Infof("EDS RES for %q clusters %v, Response: \n%s %s \n", peerAddr,
				clusters, response.String())
		} else {
			log.Infof("EDS empty clusters %v \n", peerAddr)
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
	response := Endpoints(s.mesh, clusters)
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
