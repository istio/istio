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

package discovery

import (
	"context"
	"io"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"istio.io/istio/pkg/log"
)

var (
	// Identity is a FilterFunc that always returns the original discovery response, unaltered.
	Identity = func(resp *xdsapi.DiscoveryResponse) (*xdsapi.DiscoveryResponse, error) {
		return resp, nil
	}
)

// FilterFunc is a function that allows an application to filter Envoy discovery responses
type FilterFunc func(resp *xdsapi.DiscoveryResponse) (*xdsapi.DiscoveryResponse, error)

// Filter proxies requests to an Envoy discovery service (e.g. Pilot) and filters the responses.
type Filter struct {
	// DiscoveryAddr is the address of the "real" discovery service whose responses will be filtered.
	DiscoveryAddr string

	// The filtering function to be used for processing discovery responses. If nil, Identity will be used.
	FilterFunc FilterFunc
}

// Register adds the handlers to the grpc server
func (p *Filter) Register(rpcs *grpc.Server) {
	ads.RegisterAggregatedDiscoveryServiceServer(rpcs, p)
}

// StreamAggregatedResources implements the ADS interface.
func (p *Filter) StreamAggregatedResources(stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	// Create a new connection to the ADS server.
	adsProxy, err := newAdsProxy(p.DiscoveryAddr)
	if err != nil {
		return err
	}
	defer adsProxy.stop()

	// Start a routine to receive incoming discovery requests.
	var receiveError error
	requestChannel := make(chan *xdsapi.DiscoveryRequest, 1)
	go receiveThread(stream, requestChannel, &receiveError)

	for {
		select {
		case request, ok := <-requestChannel:
			if !ok {
				// Remote side closed connection.
				return receiveError
			}
			// Forward the discovery request to the ADS server
			if err := adsProxy.stream.Send(request); err != nil {
				return err
			}
		case response, ok := <-adsProxy.responseChannel:
			if !ok {
				return adsProxy.responseError
			}
			// Apply the filtering function to the discovery response received from the ADS server.
			filteredResponse, err := p.getFilterFunc()(response)
			if err != nil {
				return err
			}
			// Send the response back to the client.
			if err := stream.Send(filteredResponse); err != nil {
				return err
			}
		}
	}
}

func (p *Filter) getFilterFunc() FilterFunc {
	// If no filtering function was supplied, use Identity.
	if p.FilterFunc != nil {
		return p.FilterFunc
	}
	return Identity
}

func receiveThread(stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer, reqChannel chan *xdsapi.DiscoveryRequest, errP *error) {
	defer close(reqChannel) // indicates close of the remote side.
	for {
		req, err := stream.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled || err == io.EOF {
				log.Infoa(err)
				return
			}
			*errP = err
			log.Errora(err)
			return
		}
		reqChannel <- req
	}
}

// adsProxy proxies ADS request/response to the ADS server
type adsProxy struct {
	conn            *grpc.ClientConn
	stream          ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient
	responseChannel chan *xdsapi.DiscoveryResponse
	responseError   error
}

func newAdsProxy(discoveryAddr string) (p *adsProxy, err error) {
	tempP := &adsProxy{
		responseChannel: make(chan *xdsapi.DiscoveryResponse, 1),
	}

	// If anything goes wrong, just stop.
	defer func() {
		if err != nil {
			tempP.stop()
		}
	}()

	// TODO(nmittler): Support secure connections.
	tempP.conn, err = grpc.Dial(discoveryAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	adsClient := ads.NewAggregatedDiscoveryServiceClient(tempP.conn)
	tempP.stream, err = adsClient.StreamAggregatedResources(context.Background())
	if err != nil {
		return nil, err
	}

	go tempP.receiveThread()
	return tempP, nil
}

func (p *adsProxy) stop() {
	if p.stream != nil {
		_ = p.stream.CloseSend()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
}

func (p *adsProxy) receiveThread() {
	defer close(p.responseChannel) // indicates close of the remote side.
	for {
		response, err := p.stream.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled || err == io.EOF {
				log.Infoa(err)
				return
			}
			p.responseError = err
			log.Errora(err)
			return
		}
		p.responseChannel <- response
	}
}
