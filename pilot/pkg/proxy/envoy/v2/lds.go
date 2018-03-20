// Copyright 2017 Istio Authors
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
	"errors"
	"io"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	google_protobuf "github.com/gogo/protobuf/types"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/deprecated"
	"istio.io/istio/pkg/log"
)

// StreamListeners implements the DiscoveryServer interface.
func (s *DiscoveryServer) StreamListeners(stream xdsapi.ListenerDiscoveryService_StreamListenersServer) error {
	log.Info("StreamListeners")
	ticker := time.NewTicker(responseTickDuration)
	peerInfo, ok := peer.FromContext(stream.Context())
	peerAddr := unknownPeerAddressStr
	if ok {
		peerAddr = peerInfo.Addr.String()
	}
	defer ticker.Stop()
	var discReq *xdsapi.DiscoveryRequest
	var receiveError error
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
				log.Errorf("request loop for LDS for client %q terminated with errors %v", peerAddr, err)
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
				log.Debugf("LDS ACK from Envoy for client %q has version %q and Nonce %q for request", discReq.GetVersionInfo(), discReq.GetResponseNonce())
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
			log.Debugf("LDS request from  %q received.", peerAddr)
		}

		nt, err := model.ParseServiceNode(discReq.Node.Id)
		if err != nil {
			return err
		}
		node := model.Proxy{
			ID:   discReq.Node.Id,
			Type: nt.Type,
		}
		response, err := ListListenersResponse(s.env, node)
		log.Info(response.String())
		if err != nil {
			return err
		}
		err = stream.Send(response)
		if err != nil {
			return err
		}
		log.Debugf("\nLDS response from  %q, Response: \n%s\n\n", peerAddr, response.String())
	}
}

// FetchListeners implements the DiscoveryServer interface.
func (s *DiscoveryServer) FetchListeners(ctx context.Context, in *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	log.Info("FetchListeners")
	node, err := model.ParseServiceNode(in.Node.Id)
	if err != nil {
		return nil, err
	}
	log.Debugf("LDSv2 request for %s.", node.ID)
	return nil, errors.New("function FetchListeners not implemented")
}

// ListListenersResponse returns a list of listeners for the given environment and source node.
func ListListenersResponse(env model.Environment, node model.Proxy) (*xdsapi.DiscoveryResponse, error) {
	ls, err := deprecated.BuildListeners(env, node)
	if err != nil {
		return nil, err
	}

	resp := &xdsapi.DiscoveryResponse{}
	for _, ll := range ls {
		lr, _ := google_protobuf.MarshalAny(ll)
		resp.Resources = append(resp.Resources, *lr)
	}

	return resp, nil
}
