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

package endpoints

import (
	"testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestHBONEOriginationShimEndpointScope(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, false)
	for _, tt := range []struct {
		name      string
		direction model.TrafficDirection
		sni       bool
		want      bool
	}{
		{name: "outbound", direction: model.TrafficDirectionOutbound, want: true},
		{name: "waypoint vip", direction: model.TrafficDirectionInboundVIP, want: true},
		{name: "inbound", direction: model.TrafficDirectionInbound},
		{name: "sni passthrough", direction: model.TrafficDirectionOutbound, sni: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := &model.Service{Hostname: "app.default.svc.cluster.local"}
			name := model.BuildSubsetKey(tt.direction, "", service.Hostname, 80)
			if tt.sni {
				name = model.BuildDNSSrvSubsetKey(tt.direction, "", service.Hostname, 80)
			}
			b := EndpointBuilder{
				clusterName: name, dir: tt.direction, service: service,
				proxy: &model.Proxy{Type: model.Router, Metadata: &model.NodeMetadata{EnableHBONEOriginationShim: true}},
			}
			b.populateHBONEOriginationShim()
			assert.Equal(t, b.hboneOriginationShim, tt.want)
		})
	}
}
