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
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/jsonpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"fmt"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

var (
	adsDebug = os.Getenv("PILOT_DEBUG_ADS") != "0"

	adsClientsMutex sync.RWMutex
	adsClients      = map[string]*XdsConnection{}
)

// XdsConnection is a listener connection type.
type XdsConnection struct {
	// PeerAddr is the address of the client envoy, from network layer
	PeerAddr string

	// Time of connection, for debugging
	Connect time.Time

	// Node is the name of the remote node
	Node string

	modelNode *model.Proxy

	// Sending on this channel results in  push. We may also make it a channel of objects so
	// same info can be sent to all clients, without recomputing.
	pushChannel chan struct{}

	// TODO: migrate other fields as needed from model.Proxy and replace it

	//HttpConnectionManagers map[string]*http_conn.HttpConnectionManager

	HTTPListeners []*xdsapi.Listener

	// current list of clusters monitored by the client
	Clusters []string

	// TODO: TcpListeners (may combine mongo/etc)

	stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer

	// LDSWatch is set if the remote server is watching Listeners
	LDSWatch bool
	// CDSWatch is set if the remote server is watching Clusters
	CDSWatch bool
	// RDSWatch is set if the remote server is watching Routes
	RDSWatch bool
}

// StreamListeners implements the DiscoveryServer interface.
func (s *DiscoveryServer) StreamAggregatedResources(stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	peerInfo, ok := peer.FromContext(stream.Context())
	peerAddr := unknownPeerAddressStr
	if ok {
		peerAddr = peerInfo.Addr.String()
	}
	var discReq *xdsapi.DiscoveryRequest
	var receiveError error
	reqChannel := make(chan *xdsapi.DiscoveryRequest, 1)
	var nodeID string

	con := &XdsConnection{
		pushChannel:   make(chan struct{}, 1),
		PeerAddr:      peerAddr,
		Connect:       time.Now(),
		HTTPListeners: []*xdsapi.Listener{},
		Clusters:      []string{},
		stream:        stream,
	}
	go func() {
		defer close(reqChannel)
		defer removeAdsCon(nodeID)
		for {
			req, err := stream.Recv()
			if err != nil {
				for _, c := range con.Clusters {
					s.removeEdsCon(c, nodeID, con)
				}
				if status.Code(err) == codes.Canceled || err == io.EOF {
					log.Infof("ADS: %q %s terminated %v", peerAddr, nodeID, err)
					return
				}
				receiveError = err
				log.Errorf("ADS: %q %s terminated with errors %v", peerAddr, nodeID, err)
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
			if discReq.TypeUrl != endpointType {
				// ADS appears to send an empty Node.Id
				if discReq.Node.Id == "" {
					log.Infof("Missing node id %s", discReq.String())
					continue
				}
				nt, err := model.ParseServiceNode(discReq.Node.Id)
				if err != nil {
					return err
				}
				con.modelNode = &nt
			}
			nodeID = connectionID(discReq.Node.Id)

			switch discReq.TypeUrl {
			case clusterType:
				if con.CDSWatch {
					// Already received a cluster watch request, this is an ACK
					if discReq.ErrorDetail != nil {
						log.Warnf("CDS: ACK ERROR %v %s %v", peerAddr, nodeID, discReq.String())
					}
					if cdsDebug {
						log.Infof("CDS: ACK %v", discReq.String())
					}
					continue
				}
				con.CDSWatch = true

			case listenerType:
				if con.LDSWatch {
					// Already received a cluster watch request, this is an ACK
					if discReq.ErrorDetail != nil {
						log.Warnf("LDS: ACK ERROR %v %s %v", peerAddr, con.modelNode.ID, discReq.String())
					}
					if cdsDebug {
						log.Infof("LDS: ACK %v", discReq.String())
					}
					continue
				}
				con.LDSWatch = true

			case routeType:
				if con.RDSWatch {
					// Already received a cluster watch request, this is an ACK
					if discReq.ErrorDetail != nil {
						log.Warnf("RDS: ACK ERROR %v %s %v", peerAddr, con.modelNode.ID, discReq.String())
					}
					if cdsDebug {
						log.Infof("RDS: ACK %v", discReq.String())
					}
					continue
				}
				con.RDSWatch = true
			case endpointType:
				clusters := discReq.GetResourceNames()
				if len(clusters) == len(con.Clusters) || len(clusters) == 0 {
					if discReq.ErrorDetail != nil {
						log.Warnf("EDS: ACK ERROR %v %s %v", peerAddr, nodeID, discReq.String())
					}
					if edsDebug {
						log.Infof("EDS: ACK %s %s %s %s", nodeID, discReq.VersionInfo, con.Clusters, discReq.String())
					}
					if len(con.Clusters) > 0 {
						// Already got a list of clusters to watch and has same length as the request, this is an ack
						continue
					}
				}
				con.Clusters = clusters
				for _, c := range con.Clusters {
					s.addEdsCon(c, nodeID, con)
				}

			default:
				log.Warnf("ADS: Unknown watched resources %s", discReq.String())
			}

			con.Node = nodeID
			addAdsCon(nodeID, con)

			if adsDebug {
				log.Infof("ADS: REQ %v %s %s", peerAddr, con.modelNode.ID, discReq.String())
			}
		case <-con.pushChannel:
			// It is called when config changes.
			// This is not optimized yet - we should detect what changed based on event and only
			// push resources that need to be pushed.
		}

		if con.CDSWatch {
			err := s.pushCds(*con.modelNode, con)
			if err != nil {
				return err
			}
		}
		if len(con.Clusters) > 0 {
			err := s.pushEds(con)
			if err != nil {
				return err
			}
		}
		if con.LDSWatch {
			err := s.pushLds(*con.modelNode, con)
			if err != nil {
				return err
			}
		}
	}
}

// ldsPushAll implements old style invalidation, generated when any rule or endpoint changes.
// Primary code path is from v1 discoveryService.clearCache(), which is added as a handler
// to the model ConfigStorageCache and Controller.
func adsPushAll() {
	adsClientsMutex.RLock()
	// Create a temp map to avoid locking the add/remove
	tmpMap := map[string]*XdsConnection{}
	for k, v := range adsClients {
		tmpMap[k] = v
	}
	adsClientsMutex.RUnlock()

	for _, client := range tmpMap {
		client.pushChannel <- struct{}{}
	}
}

// LDSz implements a status and debug interface for ADS.
// It is mapped to /debug/ldsz on the monitor port (9093).
func LDSz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	if req.Form.Get("debug") != "" {
		ldsDebug = req.Form.Get("debug") == "1"
		return
	}
	if req.Form.Get("push") != "" {
		adsPushAll()
		fmt.Fprintf(w, "Pushed to %d servers", len(adsClients))
		return
	}
	adsClientsMutex.RLock()

	//data, err := json.Marshal(ldsClients)

	// Dirty json generation - because standard json is dirty (struct madness)
	// Unfortunately we must use the jsonbp to encode part of the json - I'm sure there are
	// better ways, but this is mainly for debugging.
	fmt.Fprint(w, "[\n")
	comma2 := false
	for _, c := range adsClients {
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

	adsClientsMutex.RUnlock()

	//if err != nil {
	//	_, _ = w.Write([]byte(err.Error()))
	//	return
	//}
	//
	//_, _ = w.Write(data)
}

func addAdsCon(s string, connection *XdsConnection) {
	adsClientsMutex.Lock()
	defer adsClientsMutex.Unlock()
	adsClients[s] = connection
}

func removeAdsCon(s string) {
	adsClientsMutex.Lock()
	defer adsClientsMutex.Unlock()

	if adsClients[s] == nil {
		log.Errorf("Removing ADS connection for non-existing node %s.", s)
	}
	delete(adsClients, s)
}
