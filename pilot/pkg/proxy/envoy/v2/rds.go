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
	"time"

	"istio.io/istio/pkg/util/protomarshal"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

func (s *DiscoveryServer) pushRoute(con *XdsConnection, push *model.PushContext, version string) error {
	pushStart := time.Now()
	rawRoutes := s.generateRawRoutes(con, push)
	if s.DebugConfigs {
		for _, r := range rawRoutes {
			con.RouteConfigs[r.Name] = r
			if adsLog.DebugEnabled() {
				resp, _ := protomarshal.ToJSONWithIndent(r, " ")
				adsLog.Debugf("RDS: Adding route:%s for node:%v", resp, con.node.ID)
			}
		}
	}

	response := routeDiscoveryResponse(rawRoutes, version, push.Version)
	err := con.send(response)
	rdsPushTime.Record(time.Since(pushStart).Seconds())
	if err != nil {
		adsLog.Warnf("RDS: Send failure for node:%v: %v", con.node.ID, err)
		recordSendError(rdsSendErrPushes, err)
		return err
	}
	rdsPushes.Increment()

	adsLog.Infof("RDS: PUSH for node:%s routes:%d", con.node.ID, len(rawRoutes))
	return nil
}

func (s *DiscoveryServer) generateRawRoutes(con *XdsConnection, push *model.PushContext) []*xdsapi.RouteConfiguration {
	rawRoutes := s.ConfigGenerator.BuildHTTPRoutes(con.node, push, con.Routes)
	// Now validate each route
	for _, r := range rawRoutes {
		if err := r.Validate(); err != nil {
			adsLog.Errorf("RDS: Generated invalid routes for route:%s for node:%v: %v, %v", r.Name, con.node.ID, err, r)
			rdsBuildErrPushes.Increment()
			// Generating invalid routes is a bug.
			// Instead of panic, which will break down the whole cluster. Just ignore it here, let envoy process it.
		}
	}
	return rawRoutes
}

func routeDiscoveryResponse(rs []*xdsapi.RouteConfiguration, version string, noncePrefix string) *xdsapi.DiscoveryResponse {
	resp := &xdsapi.DiscoveryResponse{
		TypeUrl:     RouteType,
		VersionInfo: version,
		Nonce:       nonce(noncePrefix),
	}
	for _, rc := range rs {
		rr := util.MessageToAny(rc)
		resp.Resources = append(resp.Resources, rr)
	}

	return resp
}
