// Copyright Istio Authors
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

package xds

import (
	"encoding/json"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/codes"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/env"
	istioversion "istio.io/pkg/version"
)

// gen2 provides experimental support for extended generation mechanism.

// IstioControlPlaneInstance defines the format Istio uses for when creating Envoy config.core.v3.ControlPlane.identifier
type IstioControlPlaneInstance struct {
	// The Istio component type (e.g. "istiod")
	Component string
	// The ID of the component instance
	ID string
	// The Istio version
	Info istioversion.BuildInfo
}

var (
	controlPlane *corev3.ControlPlane
)

// handleReqAck checks if the message is an ack/nack and handles it, returning true.
// If false, the request should be processed by calling the generator.
func (s *DiscoveryServer) handleReqAck(con *Connection, discReq *discovery.DiscoveryRequest) (*model.WatchedResource, bool) {

	// All NACKs should have ErrorDetail set !
	// Relying on versionCode != sentVersionCode as nack is less reliable.

	isAck := true

	t := discReq.TypeUrl
	con.mu.Lock()
	w := con.node.ActiveExperimental[t]
	if w == nil {
		w = &model.WatchedResource{
			TypeUrl: t,
		}
		con.node.ActiveExperimental[t] = w
		isAck = false // newly watched resource
	}
	con.mu.Unlock()

	if discReq.ErrorDetail != nil {
		errCode := codes.Code(discReq.ErrorDetail.Code)
		adsLog.Warnf("ADS: ACK ERROR %s %s:%s", con.ConID, errCode.String(), discReq.ErrorDetail.GetMessage())
		if s.InternalGen != nil {
			s.InternalGen.OnNack(con.node, discReq)
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
		adsLog.Debugf("ADS: Expired nonce received %s, sent %s, received %s",
			con.ConID, nonceSent, discReq.ResponseNonce)
		xdsExpiredNonce.Increment()
		// This is an ACK for a resource sent on an older stream, or out of sync.
		// Send a response back.
		isAck = false
	}

	// Change in the set of watched resource - regardless of ack, send new data.
	if !listEqualUnordered(w.ResourceNames, discReq.ResourceNames) {
		isAck = false
		w.ResourceNames = discReq.ResourceNames
	}
	w.LastRequest = discReq

	return w, isAck
}

// ControlPlane identifies the instance and Istio version.
func ControlPlane() *corev3.ControlPlane {
	return controlPlane
}

func init() {
	// The Pod Name (instance identity) is in PilotArgs, but not reachable globally nor from DiscoveryServer
	podName := env.RegisterStringVar("POD_NAME", "", "").Get()
	byVersion, err := json.Marshal(IstioControlPlaneInstance{
		Component: "istiod",
		ID:        podName,
		Info:      istioversion.Info,
	})
	if err != nil {
		adsLog.Warnf("XDS: Could not serialize control plane id: %v", err)
	}
	controlPlane = &corev3.ControlPlane{Identifier: string(byVersion)}
}

// handleCustomGenerator uses model.Generator to generate the response.
func (s *DiscoveryServer) handleCustomGenerator(con *Connection, req *discovery.DiscoveryRequest) error {
	w, isAck := s.handleReqAck(con, req)
	if isAck {
		return nil
	}

	push := s.globalPushContext()
	resp := &discovery.DiscoveryResponse{
		ControlPlane: ControlPlane(),
		TypeUrl:      w.TypeUrl,
		VersionInfo:  push.Version, // TODO: we can now generate per-type version !
		Nonce:        nonce(push.Version),
	}
	if push.Version == "" { // Usually in tests.
		resp.VersionInfo = resp.Nonce
	}

	// XdsResourceGenerator is the default generator for this connection. We want to allow
	// some types to use custom generators - for example EDS.
	g := con.node.XdsResourceGenerator
	if cg, f := s.Generators[con.node.Metadata.Generator+"/"+w.TypeUrl]; f {
		g = cg
	}
	if cg, f := s.Generators[w.TypeUrl]; f {
		g = cg
	}
	if g == nil {
		g = s.Generators["api"] // default to MCS generators - any type supported by store
	}

	if g == nil {
		return nil
	}

	cl := g.Generate(con.node, push, w, nil)
	sz := 0
	for _, rc := range cl {
		resp.Resources = append(resp.Resources, rc)
		sz += len(rc.Value)
	}

	err := con.send(resp)
	if err != nil {
		recordSendError("ADS", con.ConID, apiSendErrPushes, err)
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
func (s *DiscoveryServer) pushGeneratorV2(con *Connection, push *model.PushContext,
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

	resp := &discovery.DiscoveryResponse{
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
		recordSendError("ADS", con.ConID, apiSendErrPushes, err)
		return err
	}
	w.LastSent = time.Now()
	w.LastSize = sz // just resource size - doesn't include header and types
	w.NonceSent = resp.Nonce

	adsLog.Infof("XDS: PUSH %s for node:%s resources:%d", w.TypeUrl, con.node.ID, len(cl))
	return nil
}
