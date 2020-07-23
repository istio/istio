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

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

// clusters aggregate a DiscoveryResponse for pushing.
func cdsDiscoveryResponse(response []*cluster.Cluster, noncePrefix, typeURL string) *discovery.DiscoveryResponse {
	out := &discovery.DiscoveryResponse{
		// All resources for CDS ought to be of the type Cluster
		TypeUrl: typeURL,

		// Pilot does not really care for versioning. It always supplies what's currently
		// available to it, irrespective of whether Envoy chooses to accept or reject CDS
		// responses. Pilot believes in eventual consistency and that at some point, Envoy
		// will begin seeing results it deems to be good.
		VersionInfo: versionInfo(),
		Nonce:       nonce(noncePrefix),
	}

	for _, c := range response {
		cc := util.MessageToAny(c)
		cc.TypeUrl = typeURL
		out.Resources = append(out.Resources, cc)
	}

	return out
}

func (s *DiscoveryServer) pushCds(con *Connection, push *model.PushContext, version string) error {
	// TODO: Modify interface to take services, and config instead of making library query registry
	pushStart := time.Now()
	rawClusters := s.ConfigGenerator.BuildClusters(con.node, push)

	if s.DebugConfigs {
		con.XdsClusters = rawClusters
	}
	response := cdsDiscoveryResponse(rawClusters, push.Version, con.node.RequestedTypes.CDS)
	err := con.send(response)
	cdsPushTime.Record(time.Since(pushStart).Seconds())
	if err != nil {
		recordSendError("CDS", con.ConID, cdsSendErrPushes, err)
		return err
	}
	cdsPushes.Increment()

	// The response can't be easily read due to 'any' marshaling.
	adsLog.Infof("CDS: PUSH for node:%s clusters:%d services:%d version:%s",
		con.node.ID, len(rawClusters), len(push.Services(nil)), version)
	return nil
}
