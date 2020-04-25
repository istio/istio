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
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"istio.io/istio/pilot/pkg/networking/util"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc/codes"

	"istio.io/istio/pilot/pkg/model"
)

// gen2 provides experimental support for extended generation mechanism.

func (s *DiscoveryServer) generator(proxy *model.Proxy, con *XdsConnection, discReq *xdsapi.DiscoveryRequest) model.XdsResourceGenerator {
	if proxy.Metadata.Generator != "" {
		gen := s.Generators[proxy.Metadata.Generator]
		return gen
	}
	return nil
}

// TODO:
// 1. Per resource type generator - map, method to find watched and generator
// 2. ADS implementing Generator for endpoints
// 3. DS implementing Generator for endpoints
// 4. Pass 'updated resources' to Generator

// handleReqAck checks if the message is an ack/nack and handles it, returning true.
// If false, the request should be processed by calling the generator.
func (s *DiscoveryServer) handleReqAck(con *XdsConnection, discReq *xdsapi.DiscoveryRequest) (*model.WatchedResource, bool) {

	// All NACKs should have ErrorDetail set !
	// Relying on versionCode != sentVersionCode as nack is less reliable.

	isAck := true

	t := discReq.TypeUrl
	con.mu.Lock()
	w := con.node.Active[t]
	if w == nil {
		w = &model.WatchedResource{
			TypeUrl: t,
		}
		con.node.Active[t] = w
		isAck = false // newly watched resource
	}
	con.mu.Unlock()

	if discReq.ErrorDetail != nil {
		errCode := codes.Code(discReq.ErrorDetail.Code)
		adsLog.Warnf("ADS: ACK ERROR %s %s:%s", con.ConID, errCode.String(), discReq.ErrorDetail.GetMessage())
		if s.internalGen != nil {
			s.internalGen.OnNack(con.node, discReq)
		}
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

	// XdsResourceGenerator is the default generator for this connection. We want to allow
	// some types to use custom generators - for example EDS.
	g := con.node.XdsResourceGenerator
	if cg, f := s.Generators[con.node.Metadata.Generator+"/"+w.TypeUrl]; f {
		g = cg
	}

	cl := g.Generate(con.node, push, w, nil)
	sz := 0
	for _, rc := range cl {
		resp.Resources = append(resp.Resources, rc)
		sz += len(rc.Value)
	}

	err := con.send(resp)
	if err != nil {
		adsLog.Warnf("ADS: Send failure %s: %v", con.ConID, err)
		recordSendError(apiSendErrPushes, err)
		return err
	}
	apiPushes.Increment()
	w.LastSent = time.Now()
	w.LastSize = sz // just resource size - doesn't include header and types
	w.NonceSent = resp.Nonce
	adsLog.Infof("Pushed %s to %s count=%d size=%d", w.TypeUrl, con.node.ID, len(cl), sz)

	return nil
}

// TODO: verify that ProxyNeedsPush works correctly for Generator - ie. Sidecar visibility
// is respected for arbitrary resource types.

// Called for config updates.
// Will not be called if ProxyNeedsPush returns false - ie. if the update
func (s *DiscoveryServer) pushGeneratorV2(con *XdsConnection, push *model.PushContext,
	currentVersion string, w *model.WatchedResource, updates model.XdsUpdates) error {
	// TODO: generators may send incremental changes if both sides agree on the protocol.
	// This is specific to each generator type.
	cl := con.node.XdsResourceGenerator.Generate(con.node, push, w, updates)
	if cl == nil {
		return nil // No push needed.
	}

	// TODO: add a 'version' to the result of generator. If set, use it to determine if the result
	// changed - in many cases it will not change, so we can skip the push. Also the version will
	// become dependent of the specific resource - for example in case of API it'll be the largest
	// version of the requested type.

	resp := &xdsapi.DiscoveryResponse{
		TypeUrl:     w.TypeUrl,
		VersionInfo: currentVersion,
		Nonce:       nonce(push.Version),
	}

	sz := 0
	for _, rc := range cl {
		resp.Resources = append(resp.Resources, rc)
		sz += len(rc.Value)
	}

	err := con.send(resp)
	if err != nil {
		adsLog.Warnf("ADS: Send failure %s %s: %v", w.TypeUrl, con.ConID, err)
		recordSendError(apiSendErrPushes, err)
		return err
	}
	w.LastSent = time.Now()
	w.LastSize = sz // just resource size - doesn't include header and types
	w.NonceSent = resp.Nonce

	adsLog.Infof("XDS: PUSH %s for node:%s resources:%d", w.TypeUrl, con.node.ID, len(cl))
	return nil
}

const (
	TypeURLConnect = "istio.io/connect"
	TypeURLDisconnect = "istio.io/disconnect"

	// TODO: TypeURLReady - readiness events for endpoints, agent can propagate

	// TypeURLNACK will receive messages of type DiscoveryRequest, containing
	// the 'NACK' from envoy on rejected configs. Only ID is set in metadata.
	// This includes all the info that envoy (client) provides.
	TypeURLNACK = "istio.io/nack"

)

// InternalGen is a Generator for XDS status updates: connect, disconnect, nacks, acks
type InternalGen struct {
	Server *DiscoveryServer

	// TODO: track last N Nacks and connection events, with 'version' based on timestamp.
	// On new connect, use version to send recent events since last update.
}

func (sg *InternalGen) OnConnect(node *core.Node) {
	sg.startPush(TypeURLConnect, []*any.Any{util.MessageToAny(node)})
}

func (sg *InternalGen) OnDisconnect(node *core.Node) {
	sg.startPush(TypeURLDisconnect, []*any.Any{util.MessageToAny(node)})
}

func (sg *InternalGen) OnNack(node *model.Proxy, dr *xdsapi.DiscoveryRequest) {
	// Make sure we include the ID - the DR may not include metadata
	dr.Node.Id = node.ID
	sg.startPush(TypeURLNACK, []*any.Any{util.MessageToAny(dr)})
}

// startPush is similar with DiscoveryServer.startPush() - but called directly,
// since status discovery is not driven by config change events.
// We also want connection events to be dispatched as soon as possible,
// they may be consumed by other instances of Istiod to update internal state.
func (sg *InternalGen) startPush(typeURL string, data []*any.Any) {
// Push config changes, iterating over connected envoys. This cover ADS and EDS(0.7), both share
	// the same connection table
	sg.Server.adsClientsMutex.RLock()
	// Create a temp map to avoid locking the add/remove
	pending := []*XdsConnection{}
	for _, v := range sg.Server.adsClients {
		if v.node.Active[typeURL] != nil {
			pending = append(pending, v)
		}
	}
	sg.Server.adsClientsMutex.RUnlock()

	dr := &xdsapi.DiscoveryResponse{
		TypeUrl: typeURL,
		Resources: data,
	}

	for _, p := range pending {
		p.send(dr)
	}
}

// Generate XDS responses about internal events:
// - connection status
// - NACKs
//
// We can also expose ACKS.
func (sg *InternalGen) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates model.XdsUpdates) model.Resources {
	res := []*any.Any{}
	switch w.TypeUrl {
	case TypeURLConnect:
		sg.Server.adsClientsMutex.RLock()
		// Create a temp map to avoid locking the add/remove
		for _, v := range sg.Server.adsClients {
				res = append(res, util.MessageToAny(v.meta))
		}
		sg.Server.adsClientsMutex.RUnlock()
	}
	return res
}
