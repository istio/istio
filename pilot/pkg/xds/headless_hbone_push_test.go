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

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
)

// With PILOT_ENABLE_HEADLESS_HBONE_SEND, a sidecar's headless cluster depends on its endpoints, so headless
// endpoint updates must refresh CDS for sidecars while routers keep the optimization.
func TestHeadlessHBONEEndpointUpdatePushesCDS(t *testing.T) {
	test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
	for _, tt := range []struct {
		proxyType model.NodeType
		expectCDS bool
	}{
		{proxyType: model.SidecarProxy, expectCDS: true},
		{proxyType: model.Router, expectCDS: false},
	} {
		t.Run(string(tt.proxyType), func(t *testing.T) {
			proxy := &model.Proxy{
				Type:              tt.proxyType,
				IPAddresses:       []string{"1.1.1.1"},
				ID:                "test-proxy.default",
				Metadata:          &model.NodeMetadata{},
				MergedGateway:     &model.MergedGateway{},
				PrevMergedGateway: &model.PrevMergedGateway{},
			}
			req := &model.PushRequest{
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "my-service", Namespace: "default"}),
				Reason:         model.NewReasonStats(model.HeadlessEndpointUpdate),
				Push:           &model.PushContext{},
			}
			if _, got := cdsNeedsPush(req, proxy); got != tt.expectCDS {
				t.Errorf("cdsNeedsPush() = %v, want %v", got, tt.expectCDS)
			}
		})
	}
}
