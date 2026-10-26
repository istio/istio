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
	"testing"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// TestAmbientEastWestGatewayPushOnGatewayChange verifies that Gateway changes, which routers never skip,
// are also not skipped for ambient east-west gateways: their passthrough listeners and SNI-DNAT clusters
// are built from the merged Gateway resources.
func TestAmbientEastWestGatewayPushOnGatewayChange(t *testing.T) {
	req := &model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Gateway, Name: "eastwestgateway", Namespace: "istio-system"}),
		Reason:         model.NewReasonStats(model.ConfigUpdate),
		Push:           &model.PushContext{},
	}
	cases := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{
			name:   "ambient east-west gateway",
			labels: map[string]string{label.GatewayManaged.Name: constants.ManagedGatewayEastWestControllerLabel},
			want:   true,
		},
		{
			name:   "regular waypoint",
			labels: map[string]string{label.GatewayManaged.Name: constants.ManagedGatewayMeshControllerLabel},
			want:   false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &model.Proxy{
				Type:              model.Waypoint,
				IPAddresses:       []string{"1.1.1.1"},
				ID:                "eastwestgateway.istio-system",
				Labels:            tt.labels,
				Metadata:          &model.NodeMetadata{},
				MergedGateway:     &model.MergedGateway{},
				PrevMergedGateway: &model.PrevMergedGateway{},
			}
			if got := ldsNeedsPush(proxy, req); got != tt.want {
				t.Errorf("ldsNeedsPush() = %v, want %v", got, tt.want)
			}
			if _, got := cdsNeedsPush(req, proxy); got != tt.want {
				t.Errorf("cdsNeedsPush() = %v, want %v", got, tt.want)
			}
		})
	}
}
