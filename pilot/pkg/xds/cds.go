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
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

// clusters aggregate a DiscoveryResponse for pushing.
func cdsDiscoveryResponse(response []*cluster.Cluster, noncePrefix string) *discovery.DiscoveryResponse {
	out := &discovery.DiscoveryResponse{
		// All resources for CDS ought to be of the type Cluster
		TypeUrl: v3.ClusterType,

		// Pilot does not really care for versioning. It always supplies what's currently
		// available to it, irrespective of whether Envoy chooses to accept or reject CDS
		// responses. Pilot believes in eventual consistency and that at some point, Envoy
		// will begin seeing results it deems to be good.
		VersionInfo: versionInfo(),
		Nonce:       nonce(noncePrefix),
	}

	for _, c := range response {
		out.Resources = append(out.Resources, util.MessageToAny(c))
	}

	return out
}

func (s *DiscoveryServer) pushCds(con *Connection, push *model.PushContext, version string) error {
	pushStart := time.Now()
	defer func() { cdsPushTime.Record(time.Since(pushStart).Seconds()) }()

	rawClusters := s.ConfigGenerator.BuildClusters(con.proxy, push)

	response := cdsDiscoveryResponse(rawClusters, push.Version)
	err := con.send(response)
	if err != nil {
		recordSendError("CDS", con.ConID, cdsSendErrPushes, err)
		return err
	}
	cdsPushes.Increment()

	// The response can't be easily read due to 'any' marshaling.
	adsLog.Infof("CDS: PUSH for node:%s clusters:%d services:%d version:%s",
		con.proxy.ID, len(rawClusters), len(push.Services(nil)), version)
	return nil
}
