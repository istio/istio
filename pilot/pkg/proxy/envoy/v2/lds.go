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
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gogo/protobuf/types"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	envoy_http_connection_manager = "envoy.http_connection_manager"
)

var (
	ldsDebug = len(os.Getenv("PILOT_DEBUG_LDS")) == 0

	ldsClientsMutex sync.Mutex
	ldsClients      = map[string]*LdsConnection{}
)

type LdsConnection struct {
	// PeerAddr is the address of the client envoy, from network layer
	PeerAddr string

	// Time of connection, for debugging
	Connect time.Time

	// Sending on this channel results in  push. We may also make it a channel of objects so
	// same info can be sent to all clients, without recomputing.
	pushChannel chan bool

	// TODO: migrate other fields as needed from model.Proxy and replace it

	//HttpConnectionManagers map[string]*http_conn.HttpConnectionManager

	HTTPListeners map[string]*xdsapi.Listener

	// TODO: TcpListeners (may combine mongo/etc)
}

// StreamListeners implements the DiscoveryServer interface.
func (s *DiscoveryServer) StreamListeners(stream xdsapi.ListenerDiscoveryService_StreamListenersServer) error {
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
	var nodeId string
	node := model.Proxy{}
	con := &LdsConnection{
		pushChannel:   make(chan bool, 1),
		PeerAddr:      peerAddr,
		Connect:       time.Now(),
		HTTPListeners: map[string]*xdsapi.Listener{},
	}
	go func() {
		defer close(reqChannel)
		for {
			req, err := stream.Recv()
			if err != nil {
				log.Errorf("LDS close for client %s %q terminated with errors %v",
					nodeId, peerAddr, err)
				removeLdsCon(con, nodeId)
				if status.Code(err) == codes.Canceled || err == io.EOF {
					return
				}
				receiveError = err
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

			nodeId = nt.ID
			addLdsCon(nodeId, con)
			log.Infof("LDS: StreamListeners %v %s %s", peerAddr, nt.ID, discReq.String())

		case <-ticker.C:
			if !initialRequest {
				// Ignore ticker events until the very first request is processed.
				continue
			}
		case <-con.pushChannel:
		}

		if initialRequest {
			initialRequest = false
		}

		response, err := con.ldsDiscoveryResponse(s.env, node)
		if err != nil {
			return err
		}
		err = stream.Send(response)
		if err != nil {
			return err
		}
	}
}

// ldsPushAll implements old style invalidation, generated when any rule or endpoint changes.
// Primary code path is from v1 discoveryService.clearCache(), which is added as a handler
// to the model ConfigStorageCache and Controller.
func ldsPushAll() {
	if ldsDebug {
		log.Infoa("LDS cache reset")
	}
	ldsClientsMutex.Lock()
	// Create a temp map to avoid locking the add/remove
	tmpMap := map[string]*LdsConnection{}
	for k, v := range ldsClients {
		tmpMap[k] = v
	}
	version++
	ldsClientsMutex.Unlock()

	for _, client := range tmpMap {
		client.pushChannel <- true
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
	ldsClientsMutex.Lock()
	data, err := json.Marshal(ldsClients)
	ldsClientsMutex.Unlock()
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

func removeLdsCon(connection *LdsConnection, s string) {
	ldsClientsMutex.Lock()
	defer ldsClientsMutex.Unlock()
	oldCon := ldsClients[s]
	if oldCon == connection {
		delete(ldsClients, s)
	}
}

// FetchListeners implements the DiscoveryServer interface.
func (s *DiscoveryServer) FetchListeners(ctx context.Context, in *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	return nil, errors.New("function FetchListeners not implemented")
}

func newHTTPListener(ip string, port int, name string, config *types.Struct) *xdsapi.Listener {
	return &xdsapi.Listener{
		Address: buildAddress(ip, uint32(port)),
		Name:    fmt.Sprintf("http_%s_%d", ip, port),
		FilterChains: []listener.FilterChain{
			{
				Filters: []listener.Filter{
					{
						Name:   "envoy.http_connection_manager",
						Config: config,
					},
				},
			},
		},
	}

}
