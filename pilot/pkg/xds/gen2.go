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

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
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
	if !s.shouldRespond(con, nil, req) {
		return nil
	}

	push := s.globalPushContext()
	resp := &discovery.DiscoveryResponse{
		ControlPlane: ControlPlane(),
		TypeUrl:      req.TypeUrl,
		VersionInfo:  push.Version, // TODO: we can now generate per-type version !
		Nonce:        nonce(push.Version),
	}
	if push.Version == "" { // Usually in tests.
		resp.VersionInfo = resp.Nonce
	}

	// XdsResourceGenerator is the default generator for this connection. We want to allow
	// some types to use custom generators - for example EDS.
	g := con.proxy.XdsResourceGenerator
	if cg, f := s.Generators[con.proxy.Metadata.Generator+"/"+req.TypeUrl]; f {
		g = cg
	}
	if cg, f := s.Generators[req.TypeUrl]; f {
		g = cg
	}
	if g == nil {
		g = s.Generators["api"] // default to MCS generators - any type supported by store
	}

	if g == nil {
		return nil
	}

	cl := g.Generate(con.proxy, push, con.Watched(req.TypeUrl), nil)
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

	adsLog.Infof("%s: PUSH for node:%s resources:%d", v3.GetShortType(req.TypeUrl), con.proxy.ID, len(cl))

	return nil
}

// TODO: verify that ProxyNeedsPush works correctly for Generator - ie. Sidecar visibility
// is respected for arbitrary resource types.

// Called for config updates.
// Will not be called if ProxyNeedsPush returns false - ie. if the update
func (s *DiscoveryServer) pushGeneratorV2(con *Connection, push *model.PushContext,
	currentVersion string, w *model.WatchedResource, updates model.XdsUpdates) error {
	gen := s.Generators[w.TypeUrl]
	if gen == nil {
		return nil
	}
	// TODO: generators may send incremental changes if both sides agree on the protocol.
	// This is specific to each generator type.
	cl := gen.Generate(con.proxy, push, w, updates)
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
		Resources:   cl,
	}

	err := con.send(resp)
	if err != nil {
		recordSendError("ADS", con.ConID, apiSendErrPushes, err)
		return err
	}
	adsLog.Infof("%s: PUSH for node:%s resources:%d", v3.GetShortType(w.TypeUrl), con.proxy.ID, len(cl))
	return nil
}
