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
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

func (s *DiscoveryServer) pushLds(node model.Proxy, con *XdsConnection) error {
	ls, err := s.ConfigGenerator.BuildListeners(s.env, node)
	if err != nil {
		log.Warnf("ADS: config failure, closing grpc %v", err)
		return err
	}
	con.HTTPListeners = ls
	response, err := ldsDiscoveryResponse(ls, node)
	if err != nil {
		log.Warnf("LDS: config failure, closing grpc %v", err)
		return err
	}
	err = con.send(response)
	if err != nil {
		log.Warnf("LDS: Send failure, closing grpc %v", err)
		return err
	}
	if adsDebug {
		log.Infof("LDS: PUSH for node:%s addr:%q listeners:%d", node, con.PeerAddr, len(ls))
	}
	return nil
}

// LdsDiscoveryResponse returns a list of listeners for the given environment and source node.
func ldsDiscoveryResponse(ls []*xdsapi.Listener, node model.Proxy) (*xdsapi.DiscoveryResponse, error) {
	resp := &xdsapi.DiscoveryResponse{
		TypeUrl:     ListenerType,
		VersionInfo: versionInfo(),
		Nonce:       nonce(),
	}
	for _, ll := range ls {
		if ll == nil {
			log.Errora("Nil listener ", ll)
			continue
		}
		lr, _ := types.MarshalAny(ll)
		resp.Resources = append(resp.Resources, *lr)
	}

	return resp, nil
}
