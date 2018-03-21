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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/model"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/types"

	"istio.io/istio/pilot/pkg/networking/v1alpha3"
	"istio.io/istio/pkg/log"
)

var (
	cdsDebug = len(os.Getenv("PILOT_DEBUG_CDS")) == 0

	cdsConnectionsMux sync.Mutex

	// One connection for each Envoy connected to this pilot.
	cdsConnections = map[string]*CdsConnection{}
)

// CdsConnection represents a streaming grpc connection from an envoy server.
// This is primarily intended for supporting push, but also for debug and statusz.
type CdsConnection struct {
	PeerAddr string

	// Time of connection, for debugging
	Connect time.Time

	modelNode *model.Proxy

	// Sending on this channel results in  push. We may also make it a channel of objects so
	// same info can be sent to all clients, without recomputing.
	pushChannel chan bool
}

// clusters aggregate a DiscoveryResponse for pushing.
func (con *CdsConnection) clusters(s *DiscoveryServer) *xdsapi.DiscoveryResponse {
	version := strconv.Itoa(version)
	clAssignment := &xdsapi.ClusterLoadAssignment{}
	clAssignmentRes, _ := types.MarshalAny(clAssignment)

	out := &xdsapi.DiscoveryResponse{
		// All resources for CDS ought to be of the type ClusterLoadAssignment
		TypeUrl: clAssignmentRes.GetTypeUrl(),

		// Pilot does not really care for versioning. It always supplies what's currently
		// available to it, irrespective of whether Envoy chooses to accept or reject CDS
		// responses. Pilot believes in eventual consistency and that at some point, Envoy
		// will begin seeing results it deems to be good.
		VersionInfo: version,
		Nonce:       version + "-" + time.Now().String(),
	}

	response := v1alpha3.BuildClusters(s.env, *con.modelNode)

	for _, c := range response {
		cc, _ := types.MarshalAny(c)
		out.Resources = append(out.Resources, *cc)
	}

	return out
}

// StreamClusters implements xdsapi.EndpointDiscoveryServiceServer.StreamEndpoints().
func (s *DiscoveryServer) StreamClusters(stream xdsapi.ClusterDiscoveryService_StreamClustersServer) error {
	peerInfo, ok := peer.FromContext(stream.Context())
	peerAddr := "Unknown peer address"
	if ok {
		peerAddr = peerInfo.Addr.String()
	}
	var discReq *xdsapi.DiscoveryRequest
	var receiveError error
	reqChannel := make(chan *xdsapi.DiscoveryRequest, 1)

	con := &CdsConnection{
		pushChannel: make(chan bool, 1),
		PeerAddr:    peerAddr,
		Connect:     time.Now(),
	}
	// node is the key used in the cluster map. It includes the pod name and an unique identifier,
	// since multiple envoys may connect from the same pod.
	var node string
	go func() {
		defer close(reqChannel)
		for {
			req, err := stream.Recv()
			if err != nil {
				log.Errorf("CDS close for client %s %q terminated with errors %v",
					node, peerAddr, err)

				s.removeCdsCon(node, con)
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
			if node == "" && discReq.Node != nil {
				node = connectionID(discReq.Node.Id)
			}
			nt, err := model.ParseServiceNode(discReq.Node.Id)
			if err != nil {
				return err
			}
			node := model.Proxy{
				ID:   discReq.Node.Id,
				Type: nt.Type,
			}
			con.modelNode = &node

			// Given that Pilot holds an eventually consistent data model, Pilot ignores any acknowledgements
			// from Envoy, whether they indicate ack success or ack failure of Pilot's previous responses.
			if discReq.ResponseNonce != "" {
				// TODO: once the deps are updated, log the ErrorCode if set (missing in current version)
				if cdsDebug {
					log.Infof("CDS: ACK %s %s", node, discReq.VersionInfo)
				}
				continue
			}
			// Initial request
			if cdsDebug {
				log.Infof("CDS REQ %s %vraw: %s ",
					node, peerAddr, discReq.String())
			}

		case <-con.pushChannel:
		}

		response := con.clusters(s)
		err := stream.Send(response)
		if err != nil {
			return err
		}

		if cdsDebug {
			log.Infof("CDS RES for %s %q, Response: \n%s\n",
				node, peerAddr, response.String())
		}
	}
}

// CdsPushAll implements old style invalidation, generated when any rule or endpoint changes.
func CdsPushAll() {
	if cdsDebug {
		log.Infoa("CDS cache reset")
	}
	cdsConnectionsMux.Lock()
	// Create a temp map to avoid locking the add/remove
	tmpMap := map[string]*CdsConnection{}
	for k, v := range cdsConnections {
		tmpMap[k] = v
	}
	version++
	cdsConnectionsMux.Unlock()

	for _, cdsCon := range tmpMap {
		cdsCon.pushChannel <- true
	}
}

// Cdsz implements a status and debug interface for CDS.
// It is mapped to /debug/cdsz on the monitor port (9093).
func Cdsz(w http.ResponseWriter, req *http.Request) {
	if req.Form.Get("debug") != "" {
		cdsDebug = req.Form.Get("debug") == "1"
		return
	}
	if req.Form.Get("push") != "" {
		CdsPushAll()
	}
	data, err := json.Marshal(cdsConnections)
	if err != nil {
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	_, _ = w.Write(data)
}

// FetchClusters implements xdsapi.EndpointDiscoveryServiceServer.FetchEndpoints().
func (s *DiscoveryServer) FetchClusters(ctx context.Context, req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *DiscoveryServer) removeCdsCon(node string, connection *CdsConnection) {

}
