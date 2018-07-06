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

func (s *DiscoveryServer) pushRoute(con *XdsConnection) error {
	rawRoutes, err := s.generateRawRoutes(con)
	if err != nil {
		return err
	}
	for _, r := range rawRoutes {
		con.RouteConfigs[r.Name] = r
		resp, _ := model.ToJSONWithIndent(r, " ")
		adsLog.Debugf("RDS: Adding route %s for node %s", resp, con.modelNode)
	}

	response := routeDiscoveryResponse(rawRoutes, *con.modelNode)
	err = con.send(response)
	if err != nil {
		adsLog.Warnf("ADS: RDS: Send failure for %s, closing grpc %v", con.modelNode, err)
		pushes.With(prometheus.Labels{"type": "rds_senderr"}).Add(1)
		return err
	}
	pushes.With(prometheus.Labels{"type": "rds"}).Add(1)

	adsLog.Infof("ADS: RDS: PUSH for node: %s addr:%s routes:%d", con.modelNode, con.PeerAddr, len(rawRoutes))
	return nil
}

func (s *DiscoveryServer) generateRawRoutes(con *XdsConnection) ([]*xdsapi.RouteConfiguration, error) {
	rc := make([]*xdsapi.RouteConfiguration, 0)
	// TODO: Follow this logic for other xDS resources as well
	// And cache/retrieve this info on-demand, not for every request from every proxy
	//var services []*model.Service
	//s.modelMutex.RLock()
	//services = s.services
	//s.modelMutex.RUnlock()
	//
	//proxyInstances, err := s.getServicesForEndpoint(con.modelNode)
	//if err != nil {
	//	adsLog.Warnf("ADS: RDS: Failed to retrieve proxy service instances %v", err)
	//	pushes.With(prometheus.Labels{"type": "rds_conferr"}).Add(1)
	//	return err
	//}

	// TODO: once per config update
	for _, routeName := range con.Routes {
		r, err := s.ConfigGenerator.BuildHTTPRoutes(s.env, *con.modelNode, routeName)
		if err != nil {
			retErr := fmt.Errorf("RDS: Failed to generate route %s for node %v: %v", routeName, con.modelNode, err)
			adsLog.Warnf("RDS: Failed to generate routes for route %s for node %s: %v", routeName, con.modelNode, err)
			pushes.With(prometheus.Labels{"type": "rds_builderr"}).Add(1)
			return nil, retErr
		}

		if r == nil {
			adsLog.Warnf("RDS: got nil value for route %s for node %s: %v", routeName, con.modelNode, err)
			continue
		}

		if err = r.Validate(); err != nil {
			retErr := fmt.Errorf("RDS: Generated invalid route %s for node %v: %v", routeName, con.modelNode, err)
			adsLog.Errorf("RDS: Generated invalid routes for route %s for node %s: %v, %v", routeName, con.modelNode, err, r)
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

func routeDiscoveryResponse(rs []*xdsapi.RouteConfiguration, node model.Proxy) *xdsapi.DiscoveryResponse {
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
