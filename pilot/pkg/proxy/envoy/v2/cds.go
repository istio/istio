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

// clusters aggregate a DiscoveryResponse for pushing.
func (con *XdsConnection) clusters(response []*xdsapi.Cluster) *xdsapi.DiscoveryResponse {
	out := &xdsapi.DiscoveryResponse{
		// All resources for CDS ought to be of the type ClusterLoadAssignment
		TypeUrl: ClusterType,

		// Pilot does not really care for versioning. It always supplies what's currently
		// available to it, irrespective of whether Envoy chooses to accept or reject CDS
		// responses. Pilot believes in eventual consistency and that at some point, Envoy
		// will begin seeing results it deems to be good.
		VersionInfo: versionInfo(),
		Nonce:       nonce(),
	}

	for _, c := range response {
		cc, _ := types.MarshalAny(c)
		out.Resources = append(out.Resources, *cc)
	}

	return out
}

func (s *DiscoveryServer) pushCds(con *XdsConnection, push *model.PushContext, version string) error {
	// TODO: Modify interface to take services, and config instead of making library query registry
	rawClusters, err := s.generateRawClusters(con.modelNode, push)
	if err != nil {
		return err
	}
	if s.DebugConfigs {
		con.CDSClusters = rawClusters
	}
	response := con.clusters(rawClusters)
	err = con.send(response)
	if err != nil {
		adsLog.Warnf("CDS: Send failure %s: %v", con.ConID, err)
		pushes.With(prometheus.Labels{"type": "cds_senderr"}).Add(1)
		return err
	}
	pushes.With(prometheus.Labels{"type": "cds"}).Add(1)

	// The response can't be easily read due to 'any' marshaling.
	adsLog.Infof("CDS: PUSH %s for %s %q, Clusters: %d, Services %d", version,
		con.ConID, con.PeerAddr, len(rawClusters), len(push.Services(nil)))
	return nil
}

func (s *DiscoveryServer) generateRawClusters(node *model.Proxy, push *model.PushContext) ([]*xdsapi.Cluster, error) {
	rawClusters, err := s.ConfigGenerator.BuildClusters(s.Env, node, push)
	if err != nil {
		adsLog.Warnf("CDS: Failed to generate clusters for node %s: %v", node.ID, err)
		pushes.With(prometheus.Labels{"type": "cds_builderr"}).Add(1)
		return nil, err
	}

	for _, c := range rawClusters {
		if err = c.Validate(); err != nil {
			retErr := fmt.Errorf("CDS: Generated invalid cluster for node %v: %v", node, err)
			adsLog.Errorf("CDS: Generated invalid cluster for node %s: %v, %v", node.ID, err, c)
			pushes.With(prometheus.Labels{"type": "cds_builderr"}).Add(1)
			totalXDSInternalErrors.Add(1)
			// Generating invalid clusters is a bug.
			// Panic instead of trying to recover from that, since we can't
			// assume anything about the state.
			panic(retErr.Error())
		}
	}
	return rawClusters, nil
}
