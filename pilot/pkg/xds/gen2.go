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

var SkipLogTypes = map[string]struct{}{
	v3.EndpointType: {},
}

// Called for config updates.
// Will not be called if ProxyNeedsPush returns false - ie. if the update
func (s *DiscoveryServer) pushGenerator(con *Connection, push *model.PushContext,
	gen model.XdsResourceGenerator, currentVersion string, w *model.WatchedResource, updates model.XdsUpdates) error {
	if gen == nil {
		return nil
	}
	// TODO: generators may send incremental changes if both sides agree on the protocol.
	// This is specific to each generator type.

	t0 := time.Now()
	cl := gen.Generate(con.proxy, push, w, updates)
	if cl == nil {
		// If we have nothing to send, report that we got an ACK for this version.
		if s.StatusReporter != nil {
			s.StatusReporter.RegisterEvent(con.ConID, w.TypeUrl, push.Version)
		}
		return nil // No push needed.
	}
	recordPushTime(w.TypeUrl, time.Since(t0))

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
		recordSendError(w.TypeUrl, con.ConID, err)
		return err
	}

	// Some types handle logs inside Generate, skip them here
	if _, f := SkipLogTypes[w.TypeUrl]; !f {
		adsLog.Infof("%s: PUSH for node:%s resources:%d", v3.GetShortType(w.TypeUrl), con.proxy.ID, len(cl))
	}
	return nil
}
