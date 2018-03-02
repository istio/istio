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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pkg/log"
)

/***************************  Mesh EDS Implementation **********************************/
// Implements EDS interface methods of v2.MeshDiscovery

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
