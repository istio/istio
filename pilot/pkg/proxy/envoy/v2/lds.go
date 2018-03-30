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
	"os"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

var (
	ldsDebug = os.Getenv("PILOT_DEBUG_LDS") != "0"
)

// StreamListeners implements the DiscoveryServer interface. For connection tracking it is using
// the same structure as ADS.
func (s *DiscoveryServer) StreamListeners(stream xdsapi.ListenerDiscoveryService_StreamListenersServer) error {
	peerInfo, ok := peer.FromContext(stream.Context())
	peerAddr := unknownPeerAddressStr
	if ok {
		peerAddr = peerInfo.Addr.String()
	}
	var discReq *xdsapi.DiscoveryRequest
	var receiveError error
	reqChannel := make(chan *xdsapi.DiscoveryRequest, 1)
	var nodeID string
	node := model.Proxy{}

	// true if the stream received the initial discovery request.
	initialRequestReceived := false

	con := &XdsConnection{
		pushChannel:   make(chan struct{}, 1),
		PeerAddr:      peerAddr,
		Connect:       time.Now(),
		HTTPListeners: []*xdsapi.Listener{},
		stream: stream,
	}
	go func() {
		defer close(reqChannel)
		defer removeAdsCon(nodeID)
		for {
			req, err := stream.Recv()
			if err != nil {
				log.Errorf("LDS close for client %s %q terminated with errors %v",
					nodeID, peerAddr, err)
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
			nt, err := model.ParseServiceNode(discReq.Node.Id)
			if err != nil {
				return err
			}
			node = nt
			if initialRequestReceived {
				if discReq.ErrorDetail != nil {
					log.Warnf("LDS: ACK ERROR %v %s %v", peerAddr, nt.ID, discReq.String())
				}
				if ldsDebug {
					log.Infof("LDS: ACK %v", discReq.String())
				}
				continue
			}
			initialRequestReceived = true
			nodeID = connectionID("LDS_" + nt.ID)
			con.Node = nodeID
			con.LDSWatch = true
			con.modelNode = &nt
			addAdsCon(nodeID, con)

			if ldsDebug {
				log.Infof("LDS: REQ %v %s %s", peerAddr, nt.ID, discReq.String())
			}
		case <-con.pushChannel:
		}

		err := s.pushLds(node, con)
		if err != nil {
			return err
		}
		}
}

func (s *DiscoveryServer) pushLds(node model.Proxy, con *XdsConnection) error {
	ls, err := s.ConfigGenerator.BuildListeners(s.env, node)
	if err != nil {
		log.Warnf("ADS: config failure, closing grpc %v", err)
		return err
	}
	con.HTTPListeners = ls
	response, err := ldsDiscoveryResponse(ls, node)
	if err != nil {
		log.Warnf("LDS: config failure, closing grpc %v", err)
		return err
	}
	err = con.stream.Send(response)
	if err != nil {
		log.Warnf("LDS: Send failure, closing grpc %v", err)
		return err
	}
	if ldsDebug {
		log.Infof("LDS: PUSH for node:%s addr:%q listeners:%d", node, con.PeerAddr, len(ls))
	}
	return nil
}


// ldsPushAll implements old style invalidation, generated when any rule or endpoint changes.
// Primary code path is from v1 discoveryService.clearCache(), which is added as a handler
// to the model ConfigStorageCache and Controller.
func ldsPushAll() {
	adsClientsMutex.RLock()
	// Create a temp map to avoid locking the add/remove
	tmpMap := map[string]*XdsConnection{}
	for k, v := range adsClients {
		if (v.LDSWatch) {
			tmpMap[k] = v
		}
	}
	adsClientsMutex.RUnlock()

	for _, client := range tmpMap {
		client.pushChannel <- struct{}{}
	}
}

// FetchListeners implements the DiscoveryServer interface.
func (s *DiscoveryServer) FetchListeners(ctx context.Context, in *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	return nil, errors.New("function FetchListeners not implemented")
}

// LdsDiscoveryResponse returns a list of listeners for the given environment and source node.
func ldsDiscoveryResponse(ls []*xdsapi.Listener, node model.Proxy) (*xdsapi.DiscoveryResponse, error) {
	resp := &xdsapi.DiscoveryResponse{
		TypeUrl:     listenerType,
		VersionInfo: versionInfo(),
		Nonce:       nonce(),
	}
	for _, ll := range ls {
		lr, _ := types.MarshalAny(ll)
		resp.Resources = append(resp.Resources, *lr)
	}

	return resp, nil
}
