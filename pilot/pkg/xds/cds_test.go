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

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestCDS(t *testing.T) {
	s := NewFakeDiscoveryServer(t, FakeOptions{})
	ads := s.ConnectADS().WithType(v3.ClusterType)
	ads.RequestResponseAck(t, nil)
}

// Regression test for https://github.com/istio/istio/issues/28402. Ensure CDS and EDS logic are in sync
func TestEDSAfterCDS(t *testing.T) {
	for _, s := range collections.All.All() {
		gvk := s.Resource().GroupVersionKind()
		for _, tt := range []model.NodeType{model.Router, model.SidecarProxy} {
			for _, full := range []bool{false, true} {
				req := &model.PushRequest{Full: full, ConfigsUpdated: map[model.ConfigKey]struct{}{
					{Kind: gvk}: {},
				}}
				proxy := &model.Proxy{Type: tt}
				cds := cdsNeedsPush(req, proxy)
				eds := edsNeedsPush(req, proxy)
				if cds && !eds {
					t.Errorf("Expected EDS push after CDS push (type=%v, gvk=%v, full=%v)", tt, gvk, full)
				}
				if full {
					if !cds && eds {
						t.Logf("Got EDS without CDS push (type=%v, gvk=%v, full=%v)", tt, gvk, full)
					}
				}
			}
		}
	}
}
