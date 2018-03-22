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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/gogo/protobuf/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/deprecated"
	"istio.io/istio/pkg/log"
)

const (
	ldsType = cache.ListenerType
)

var (
	ldsDebug = len(os.Getenv("PILOT_DEBUG_LDS")) == 0

	ldsClientsMutex sync.RWMutex
	ldsClients      = map[string]*LdsConnection{}
)

// LdsConnection is a listener connection type.
type LdsConnection struct {
	// PeerAddr is the address of the client envoy, from network layer
	PeerAddr string

	// Time of connection, for debugging
	Connect time.Time

	// Sending on this channel results in  push. We may also make it a channel of objects so
	// same info can be sent to all clients, without recomputing.
	PushChannel chan struct{}

	// TODO: migrate other fields as needed from model.Proxy and replace it

	//HttpConnectionManagers map[string]*http_conn.HttpConnectionManager

	HTTPListeners map[string]*xdsapi.Listener

	// TODO: TcpListeners (may combine mongo/etc)
}

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
	var nodeID string
	node := model.Proxy{}
	con := &LdsConnection{
		PushChannel:   make(chan struct{}, 1),
		PeerAddr:      peerAddr,
		Connect:       time.Now(),
		HTTPListeners: map[string]*xdsapi.Listener{},
	}
	go func() {
		defer close(reqChannel)
		defer removeLdsCon(nodeID)
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
			if !initialRequest {
				log.Debugf("LDS: ACK from Envoy for client %q has version %q and Nonce %q for request", discReq.GetVersionInfo(), discReq.GetResponseNonce())
				continue
			}
			node.ID = discReq.Node.Id
			node.Type = nt.Type

			nodeID = nt.ID
			addLdsCon(nodeID, con)
			log.Infof("LDS: StreamListeners2 %v %s %s", peerAddr, nt.ID, discReq.String())

		case <-ticker.C:
			if initialRequest {
				// Ignore ticker events until the very first request is processed.
				continue
			}
		case <-con.PushChannel:
		}

		response, err := LdsDiscoveryResponse(s.env, node)
		if err != nil {
			return err
		}
		err = stream.Send(response)
		if err != nil {
			return err
		}
		initialRequest = false
	}
}

// ldsPushAll implements old style invalidation, generated when any rule or endpoint changes.
// Primary code path is from v1 discoveryService.clearCache(), which is added as a handler
// to the model ConfigStorageCache and Controller.
func ldsPushAll() {
	if ldsDebug {
		log.Infoa("LDS cache reset")
	}
	ldsClientsMutex.RLock()
	// Create a temp map to avoid locking the add/remove
	tmpMap := map[string]*LdsConnection{}
	for k, v := range ldsClients {
		tmpMap[k] = v
	}
	version++
	ldsClientsMutex.RUnlock()

	for _, client := range tmpMap {
		client.PushChannel <- struct{}{}
	}
}

// LDSz implements a status and debug interface for LDS.
// It is mapped to /debug/ldsz on the monitor port (9093).
func LDSz(w http.ResponseWriter, req *http.Request) {
	if req.Form.Get("debug") != "" {
		ldsDebug = req.Form.Get("debug") == "1"
		return
	}
	if req.Form.Get("push") != "" {
		ldsPushAll()
	}
	ldsClientsMutex.RLock()
	data, err := json.Marshal(ldsClients)
	ldsClientsMutex.RUnlock()
	if err != nil {
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	_, _ = w.Write(data)
}

func addLdsCon(s string, connection *LdsConnection) {
	ldsClientsMutex.Lock()
	defer ldsClientsMutex.Unlock()
	ldsClients[s] = connection
}

func removeLdsCon(s string) {
	ldsClientsMutex.Lock()
	defer ldsClientsMutex.Unlock()

	if ldsClients[s] == nil {
		log.Errorf("Removing LDS connection for non-existing node %s.", s)
	}
	delete(ldsClients, s)
}

// FetchListeners implements the DiscoveryServer interface.
func (s *DiscoveryServer) FetchListeners(ctx context.Context, in *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	return nil, errors.New("function FetchListeners not implemented")
}

// LdsDiscoveryResponse returns a list of listeners for the given environment and source node.
func LdsDiscoveryResponse(env model.Environment, node model.Proxy) (*xdsapi.DiscoveryResponse, error) {
	ls, err := deprecated.BuildListeners(env, node)
	if err != nil {
		return nil, err
	}
	log.Infof("LDS: %s %s %s: \n%v", node.ID, node.IPAddress, node.Type, ls)
	resp := &xdsapi.DiscoveryResponse{TypeUrl: ldsType}
	for _, ll := range ls {
		lr, _ := types.MarshalAny(ll)
		resp.Resources = append(resp.Resources, *lr)
	}

	return resp, nil
}
