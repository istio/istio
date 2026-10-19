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

package util

import (
	"fmt"
	"testing"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestHBONEDoubleShimEligibility(t *testing.T) {
	for _, kind := range []model.NodeType{model.Waypoint, model.Router, model.SidecarProxy} {
		for _, global := range []bool{false, true} {
			for _, native := range []bool{false, true} {
				for _, send := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/global=%t/native=%t/send=%t", kind, global, native, send), func(t *testing.T) {
						test.SetForTest(t, &features.EnableAmbientMultiNetwork, global)
						test.SetForTest(t, &features.EnableAmbientWaypointMultiNetwork, native)
						test.SetForTest(t, &features.EnableAmbientIngressMultiNetwork, native)
						test.SetForTest(t, &features.EnableHBONESend, send)
						controller := constants.ManagedGatewayMeshControllerLabel
						if kind == model.Router {
							controller = constants.ManagedGatewayControllerLabel
						}
						p := &model.Proxy{
							Type: kind, Labels: map[string]string{label.GatewayManaged.Name: controller},
							Metadata: &model.NodeMetadata{EnableHBONEOriginationShim: true},
						}
						resources := global && native && (kind == model.Waypoint || (kind == model.Router && send))
						assert.Equal(t, model.ShouldCreateDoubleHBONEResources(p), resources)
						want := (kind == model.Waypoint || send) && (!global || resources)
						assert.Equal(t, HBONEOriginationShimEnabled(p), want)
						p.Metadata.EnableHBONEOriginationShim = false
						assert.Equal(t, HBONEOriginationShimEnabled(p), false)
						p.Metadata.EnableHBONEOriginationShim = true
						p.Metadata.DisableHBONESend = true
						assert.Equal(t, HBONEOriginationShimEnabled(p), false)
						p.Metadata.DisableHBONESend = false
						if global {
							p.Labels = nil
							assert.Equal(t, HBONEOriginationShimEnabled(p), false)
							p.Labels = map[string]string{label.GatewayManaged.Name: constants.ManagedGatewayEastWestControllerLabel}
							assert.Equal(t, HBONEOriginationShimEnabled(p), false)
						}
					})
				}
			}
		}
	}
}
