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

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

func (s *DiscoveryServer) pushLds(con *XdsConnection, push *model.PushContext, version string) error {
	// TODO: Modify interface to take services, and config instead of making library query registry
	pushStart := time.Now()
	rawListeners := s.generateRawListeners(con, push)

	if s.DebugConfigs {
		con.LDSListeners = rawListeners
	}
	response := ldsDiscoveryResponse(rawListeners, version, push.Version)
	err := con.send(response)
	ldsPushTime.Record(time.Since(pushStart).Seconds())
	if err != nil {
		adsLog.Warnf("LDS: Send failure %s: %v", con.ConID, err)
		recordSendError(ldsSendErrPushes, err)
		return err
	}
	ldsPushes.Increment()

	adsLog.Infof("LDS: PUSH for node:%s listeners:%d", con.node.ID, len(rawListeners))
	return nil
}

func (s *DiscoveryServer) generateRawListeners(con *XdsConnection, push *model.PushContext) []*xdsapi.Listener {
	rawListeners := s.ConfigGenerator.BuildListeners(con.node, push)

	for _, l := range rawListeners {
		if err := l.Validate(); err != nil {
			adsLog.Errorf("LDS: Generated invalid listener for node:%s: %v, %v", con.node.ID, err, l)
			ldsBuildErrPushes.Increment()
			// Generating invalid listeners is a bug.
			// Instead of panic, which will break down the whole cluster. Just ignore it here, let envoy process it.
		}
	}
	return rawListeners
}

// LdsDiscoveryResponse returns a list of listeners for the given environment and source node.
func ldsDiscoveryResponse(ls []*xdsapi.Listener, version string, noncePrefix string) *xdsapi.DiscoveryResponse {
	resp := &xdsapi.DiscoveryResponse{
		TypeUrl:     ListenerType,
		VersionInfo: version,
		Nonce:       nonce(noncePrefix),
	}
	for _, ll := range ls {
		if ll == nil {
			adsLog.Errora("Nil listener ", ll)
			totalXDSInternalErrors.Increment()
			continue
		}
		lr := util.MessageToAny(ll)
		resp.Resources = append(resp.Resources, lr)
	}

	return resp
}
