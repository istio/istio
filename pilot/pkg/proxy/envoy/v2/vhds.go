// Copyright 2019 Istio Authors
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

func (s *DiscoveryServer) pushDeltaVirtualHost(con *XdsConnection, push *model.PushContext, removedHosts []string) error {
	rawVHosts, err := s.generateRawVhosts(con, push)
	if err != nil {
		return err
	}

	response := deltaVirtualHostDiscoveryResponse(rawVHosts, removedHosts)
	err = con.sendDelta(response)
	if err != nil {
		adsLog.Warnf("ADS: VHDS: Send failure for node %v, closing grpc %v", con.modelNode, err)
		pushes.With(prometheus.Labels{"type": "vhds_senderr"}).Add(1)
		return err
	}
	pushes.With(prometheus.Labels{"type": "vhds"}).Add(1)

	adsLog.Infof("ADS: VHDS: PUSH for node: %s addr:%s routes:%d", con.modelNode.ID, con.PeerAddr, len(rawVHosts))
	return nil
}

//BAVERY_TODO: Rewrite this. Logic concerning route should be in here, but need route name....
func deltaVirtualHostDiscoveryResponse(resources []*xdsapi.Resource, removedHosts []string) *xdsapi.DeltaDiscoveryResponse {
	resp := &xdsapi.DeltaDiscoveryResponse{
		Nonce:             nonce(),
		SystemVersionInfo: versionInfo(),
	}

	for _, resource := range resources {
		resp.Resources = append(resp.Resources, *resource)
		}

	resp.RemovedResources = removedHosts
	return resp
}

func (s *DiscoveryServer) generateRawVhosts(con *XdsConnection, push *model.PushContext) ([]*xdsapi.Resource, error) {
	rc := make([]*xdsapi.Resource, 0)
	// TODO: Follow this logic for other xDS resources as well
	// TODO: once per config update

	for _, routeName := range con.Routes {
		virtualHosts, err := s.ConfigGenerator.BuildVirtualHosts(s.Env, con.modelNode, push, routeName)
		if err != nil {
			adsLog.Errorf("Error generating route config for %s: %s", routeName, err.Error())
			continue
		} else if virtualHosts == nil {
			adsLog.Warnf("No route config found for route %s\n", routeName)
			continue
		}

		for _, virtualHost := range virtualHosts {
			//Bavery_TODO: Marshal *should* take place just before we send....
			marshaledVirtualHost, err := types.MarshalAny(&virtualHost)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal virtual host: %+v", err.Error())
			}
			resource := &xdsapi.Resource {
				Name: routeName + "$" + virtualHost.Name,
				Aliases: virtualHost.Domains,
				//BAVERY_TODO: Address version
				//Version:
				Resource: marshaledVirtualHost,
		}
		rc = append(rc, resource)
		}
	}
	return rc, nil
}
