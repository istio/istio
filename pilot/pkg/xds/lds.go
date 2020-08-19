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
	"time"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

func (s *DiscoveryServer) pushLds(con *Connection, push *model.PushContext, version string) error {
	pushStart := time.Now()
	defer func() { ldsPushTime.Record(time.Since(pushStart).Seconds()) }()

	rawListeners := s.ConfigGenerator.BuildListeners(con.proxy, push)
	response := ldsDiscoveryResponse(rawListeners, version, push.Version)
	err := con.send(response)
	if err != nil {
		recordSendError("LDS", con.ConID, ldsSendErrPushes, err)
		return err
	}
	ldsPushes.Increment()

	adsLog.Infof("LDS: PUSH for node:%s listeners:%d", con.proxy.ID, len(rawListeners))
	return nil
}

// LdsDiscoveryResponse returns a list of listeners for the given environment and source node.
func ldsDiscoveryResponse(ls []*listener.Listener, version, noncePrefix string) *discovery.DiscoveryResponse {
	resp := &discovery.DiscoveryResponse{
		TypeUrl:     v3.ListenerType,
		VersionInfo: version,
		Nonce:       nonce(noncePrefix),
	}
	for _, ll := range ls {
		if ll == nil {
			adsLog.Errora("Nil listener ", ll)
			totalXDSInternalErrors.Increment()
			continue
		}
		resp.Resources = append(resp.Resources, util.MessageToAny(ll))
	}

	return resp
}
