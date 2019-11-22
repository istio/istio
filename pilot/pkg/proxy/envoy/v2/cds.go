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

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

// clusters aggregate a DiscoveryResponse for pushing.
func (conn *XdsConnection) clusters(response []*xdsapi.Cluster, noncePrefix string) *xdsapi.DiscoveryResponse {
	out := &xdsapi.DiscoveryResponse{
		// All resources for CDS ought to be of the type ClusterLoadAssignment
		TypeUrl: ClusterType,

		// Pilot does not really care for versioning. It always supplies what's currently
		// available to it, irrespective of whether Envoy chooses to accept or reject CDS
		// responses. Pilot believes in eventual consistency and that at some point, Envoy
		// will begin seeing results it deems to be good.
		VersionInfo: versionInfo(),
		Nonce:       nonce(noncePrefix),
	}

	for _, c := range response {
		cc := util.MessageToAny(c)
		out.Resources = append(out.Resources, cc)
	}

	return out
}

func (s *DiscoveryServer) pushCds(con *XdsConnection, push *model.PushContext, version string) error {
	// TODO: Modify interface to take services, and config instead of making library query registry
	pushStart := time.Now()
	rawClusters := s.generateRawClusters(con.node, push)

	if s.DebugConfigs {
		con.CDSClusters = rawClusters
	}
	response := con.clusters(rawClusters, push.Version)
	err := con.send(response)
	cdsPushTime.Record(time.Since(pushStart).Seconds())
	if err != nil {
		adsLog.Warnf("CDS: Send failure %s: %v", con.ConID, err)
		recordSendError(cdsSendErrPushes, err)
		return err
	}
	cdsPushes.Increment()

	// The response can't be easily read due to 'any' marshaling.
	adsLog.Infof("CDS: PUSH for node:%s clusters:%d services:%d version:%s",
		con.node.ID, len(rawClusters), len(push.Services(nil)), version)
	return nil
}

func (s *DiscoveryServer) generateRawClusters(node *model.Proxy, push *model.PushContext) []*xdsapi.Cluster {
	rawClusters := s.ConfigGenerator.BuildClusters(node, push)

	for _, c := range rawClusters {
		if err := c.Validate(); err != nil {
			adsLog.Errorf("CDS: Generated invalid cluster for node:%s: %v, %v", node.ID, err, c)
			cdsBuildErrPushes.Increment()
			totalXDSInternalErrors.Increment()
			// Generating invalid clusters is a bug.
			// Instead of panic, which will break down the whole cluster. Just ignore it here, let envoy process it.
		}
	}
	return rawClusters
}
