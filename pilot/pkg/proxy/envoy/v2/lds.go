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
	"net/http"
	"os"
	"sync"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"fmt"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/v1alpha3"
	"istio.io/istio/pkg/log"
)

var (
	ldsDebug = os.Getenv("PILOT_DEBUG_LDS") != "0"

	ldsClientsMutex sync.RWMutex
	ldsClients      = map[string]*LdsConnection{}
)

// LdsConnection is a listener connection type.
type LdsConnection struct {
	// PeerAddr is the address of the client envoy, from network layer
	PeerAddr string

	// Time of connection, for debugging
	Connect time.Time

	// Node is the name of the remote node
	Node string

	// Sending on this channel results in  push. We may also make it a channel of objects so
	// same info can be sent to all clients, without recomputing.
	pushChannel chan struct{}

	// TODO: migrate other fields as needed from model.Proxy and replace it

	//HttpConnectionManagers map[string]*http_conn.HttpConnectionManager

	HTTPListeners []*xdsapi.Listener

	// TODO: TcpListeners (may combine mongo/etc)
}

// StreamListeners implements the DiscoveryServer interface.
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

	con := &LdsConnection{
		pushChannel:   make(chan struct{}, 1),
		PeerAddr:      peerAddr,
		Connect:       time.Now(),
		HTTPListeners: []*xdsapi.Listener{},
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
			nodeID = nt.ID
			con.Node = nodeID
			addLdsCon(nodeID, con)

			if ldsDebug {
				log.Infof("LDS: REQ %v %s %s", peerAddr, nt.ID, discReq.String())
			}
		case <-con.pushChannel:
		}

		ls, err := v1alpha3.BuildListeners(s.env, node)
		if err != nil {
			log.Warnf("LDS: config failure, closing grpc %v", err)
			return err
		}
		con.HTTPListeners = ls
		response, err := ldsDiscoveryResponse(ls, node)
		if err != nil {
			log.Warnf("LDS: config failure, closing grpc %v", err)
			return err
		}
		err = stream.Send(response)
		if err != nil {
			log.Warnf("LDS: Send failure, closing grpc %v", err)
			return err
		}
		if ldsDebug {
			log.Infof("LDS: PUSH for node:%s addr:%q listeners:%d", node, peerAddr, len(ls))
		}

	}
}

// ldsPushAll implements old style invalidation, generated when any rule or endpoint changes.
// Primary code path is from v1 discoveryService.clearCache(), which is added as a handler
// to the model ConfigStorageCache and Controller.
func ldsPushAll() {
	ldsClientsMutex.RLock()
	// Create a temp map to avoid locking the add/remove
	tmpMap := map[string]*LdsConnection{}
	for k, v := range ldsClients {
		tmpMap[k] = v
	}
	ldsClientsMutex.RUnlock()

	for _, client := range tmpMap {
		client.pushChannel <- struct{}{}
	}
}

// LDSz implements a status and debug interface for LDS.
// It is mapped to /debug/ldsz on the monitor port (9093).
func LDSz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	if req.Form.Get("debug") != "" {
		ldsDebug = req.Form.Get("debug") == "1"
		return
	}
	if req.Form.Get("push") != "" {
		ldsPushAll()
		fmt.Fprintf(w, "Pushed to %d servers", len(ldsClients))
		return
	}
	ldsClientsMutex.RLock()

	//data, err := json.Marshal(ldsClients)

	// Dirty json generation - because standard json is dirty (struct madness)
	// Unfortunately we must use the jsonbp to encode part of the json - I'm sure there are
	// better ways, but this is mainly for debugging.
	fmt.Fprint(w, "[\n")
	comma2 := false
	for _, c := range ldsClients {
		if comma2 {
			fmt.Fprint(w, ",\n")
		} else {
			comma2 = true
		}
		fmt.Fprintf(w, "\n\n  {\"node\": \"%s\", \"addr\": \"%s\", \"connect\": \"%v\",\"listeners\":[\n", c.Node, c.PeerAddr, c.Connect)
		comma1 := false
		for _, ls := range c.HTTPListeners {
			if comma1 {
				fmt.Fprint(w, ",\n")
			} else {
				comma1 = true
			}
			jsonm := &jsonpb.Marshaler{}
			dbgString, _ := jsonm.MarshalToString(ls)
			if _, err := w.Write([]byte(dbgString)); err != nil {
				return
			}
		}
		fmt.Fprint(w, "]}\n")
	}
	fmt.Fprint(w, "]\n")

	ldsClientsMutex.RUnlock()

	//if err != nil {
	//	_, _ = w.Write([]byte(err.Error()))
	//	return
	//}
	//
	//_, _ = w.Write(data)
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
