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
	"fmt"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/types"
	"github.com/prometheus/client_golang/prometheus"

	"istio.io/istio/pilot/pkg/model"
)

func (s *DiscoveryServer) pushRoute(con *XdsConnection, push *model.PushContext) error {
	rawRoutes, err := s.generateRawRoutes(con, push)
	if err != nil {
		return err
	}
	if s.DebugConfigs {
		for _, r := range rawRoutes {
			con.RouteConfigs[r.Name] = r
			if adsLog.DebugEnabled() {
				resp, _ := model.ToJSONWithIndent(r, " ")
				adsLog.Debugf("RDS: Adding route %s for node %v", resp, con.modelNode)
			}
		}
	}

	response := routeDiscoveryResponse(rawRoutes)
	err = con.send(response)
	if err != nil {
		adsLog.Warnf("ADS: RDS: Send failure %v: %v", con.modelNode.ID, err)
		pushes.With(prometheus.Labels{"type": "rds_senderr"}).Add(1)
		return err
	}
	pushes.With(prometheus.Labels{"type": "rds"}).Add(1)

	adsLog.Infof("ADS: RDS: PUSH for node: %s addr:%s routes:%d", con.modelNode.ID, con.PeerAddr, len(rawRoutes))
	return nil
}

func (s *DiscoveryServer) generateRawRoutes(con *XdsConnection, push *model.PushContext) ([]*xdsapi.RouteConfiguration, error) {
	rc := make([]*xdsapi.RouteConfiguration, 0)
	// TODO: Follow this logic for other xDS resources as well
	// TODO: once per config update
	for _, routeName := range con.Routes {
		r, err := s.ConfigGenerator.BuildHTTPRoutes(s.Env, con.modelNode, push, routeName)
		if err != nil {
			retErr := fmt.Errorf("RDS: Failed to generate route %s for node %v: %v", routeName, con.modelNode, err)
			adsLog.Warnf("RDS: Failed to generate routes for route %s for node %v: %v", routeName, con.modelNode, err)
			pushes.With(prometheus.Labels{"type": "rds_builderr"}).Add(1)
			return nil, retErr
		}

		if r == nil {
			adsLog.Warnf("RDS: got nil value for route %s for node %v: %v", routeName, con.modelNode, err)
			continue
		}

		if err = r.Validate(); err != nil {
			retErr := fmt.Errorf("RDS: Generated invalid route %s for node %v: %v", routeName, con.modelNode, err)
			adsLog.Errorf("RDS: Generated invalid routes for route %s for node %v: %v, %v", routeName, con.modelNode, err, r)
			pushes.With(prometheus.Labels{"type": "rds_builderr"}).Add(1)
			// Generating invalid routes is a bug.
			// Panic instead of trying to recover from that, since we can't
			// assume anything about the state.
			panic(retErr.Error())
		}
		rc = append(rc, r)
	}
	return rc, nil
}

func routeDiscoveryResponse(rs []*xdsapi.RouteConfiguration) *xdsapi.DiscoveryResponse {
	resp := &xdsapi.DiscoveryResponse{
		TypeUrl:     RouteType,
		VersionInfo: versionInfo(),
		Nonce:       nonce(),
	}
	for _, rc := range rs {
		rr, _ := types.MarshalAny(rc)
		resp.Resources = append(resp.Resources, *rr)
	}

	return resp
}
