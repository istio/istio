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
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/codes"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/grpcgen"
)

// gen2 provides experimental support for extended generation mechanism.

// Support generation of 'ApiListener' LDS responses, used for native support of gRPC.
// The same response can also be used by other apps using XDS directly.

// GRPC proposal:
// https://github.com/grpc/proposal/blob/master/A27-xds-global-load-balancing.md
//
// Note that this implementation is tested against gRPC, but it is generic - any other framework can
// use this XDS mode to get load balancing info from Istio, including MC/VM/etc.

// DNS can populate the name to cluster VIP mapping using this response.

// The corresponding RDS response is also generated - currently gRPC has special differences
// and can't understand normal Istio RDS - in particular expects "" instead of "/" as
// default prefix, and is expects just the route for one host.
// handleReqAck will detect if the message is an ACK or NACK, and update/log/count
// using the generic structures. "Classical" CDS/LDS/RDS/EDS use separate logic -
// this is used for the API-based LDS and generic messages.



var (
	// Interface is slightly different for now - LDS/CDS have extra param.
	gen = &grpcgen.GrpcConfigGenerator{}
)

// handleReqAck checks if the message is an ack/nack and handles it, returning true.
// If false, the request should be processed by calling the generator.
func (s *DiscoveryServer) handleReqAck(con *XdsConnection, discReq *xdsapi.DiscoveryRequest) (*model.WatchedResource, bool) {

	// All NACKs should have ErrorDetail set !
	// Relying on versionCode != sentVersionCode as nack is less reliable.

	isAck := true

	t := discReq.TypeUrl
	con.mu.RLock()
	w := con.node.Active[t]
	if w == nil {
		w = &model.WatchedResource{
			TypeUrl:  t,
		}
		con.node.Active[t] = w
		isAck = false // newly watched resource
	}
	con.mu.RUnlock()

	if discReq.ErrorDetail != nil {
		errCode := codes.Code(discReq.ErrorDetail.Code)
		adsLog.Warnf("ADS: ACK ERROR %s %s:%s", con.ConID, errCode.String(), discReq.ErrorDetail.GetMessage())
		return w, true
	}

	if discReq.ResponseNonce == "" {
		isAck = false // initial request
	}
	// This is an ACK response to a previous message - but it may refer to a response on a previous connection to
	// a different XDS server instance.
	nonceSent := w.NonceSent

	// GRPC doesn't send version info in NACKs for RDS. Technically if nonce matches
	// previous response, it is an ACK/NACK.
	if nonceSent != "" && nonceSent == discReq.ResponseNonce {
		adsLog.Debugf("ADS: ACK %s %s %s %v", con.ConID, discReq.VersionInfo, discReq.ResponseNonce,
			time.Since(w.LastSent))
		w.NonceAcked = discReq.ResponseNonce
	}

	if nonceSent != discReq.ResponseNonce {
		adsLog.Debugf("ADS:RDS: Expired nonce received %s, sent %s, received %s",
			con.ConID, nonceSent, discReq.ResponseNonce)
		rdsExpiredNonce.Increment()
		// This is an ACK for a resource sent on an older stream, or out of sync.
		// Send a response back.
		isAck = false
	}

	// Change in the set of watched resource - regardless of ack, send new data.
	if !listEqualUnordered(w.ResourceNames, discReq.ResourceNames) {
		isAck = false
		w.ResourceNames = discReq.ResourceNames
	}

	return w, isAck
}


// handleCustomGenerator uses model.Generator to generate the response.
func (s *DiscoveryServer) handleCustomGenerator(con *XdsConnection, req *xdsapi.DiscoveryRequest) error {
	w, isAck := s.handleReqAck(con, req)
	if isAck {
		return nil
	}

	push := s.globalPushContext()
	resp := &xdsapi.DiscoveryResponse{
		TypeUrl:     w.TypeUrl,
		VersionInfo: push.Version, // TODO: we can now generate per-type version !
		Nonce:       nonce(push.Version),
	}

	cl := con.node.Generator.Generate(con.node, push, w)
	for _, rc := range cl {
		resp.Resources = append(resp.Resources, rc)
	}

	err := con.send(resp)
	if err != nil {
		adsLog.Warnf("ADS: Send failure %s: %v", con.ConID, err)
		recordSendError(apiSendErrPushes, err)
		return err
	}
	w.LastSent = time.Now()
	w.LastSize = proto.Size(resp)
	w.NonceSent = resp.Nonce

	return nil
}
