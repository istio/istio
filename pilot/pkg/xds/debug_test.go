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

package xds_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

func TestSyncz(t *testing.T) {
	t.Run("return the sent and ack status of adsClient connections", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		ads := s.ConnectADS()

		ads.RequestResponseAck(t, &discovery.DiscoveryRequest{TypeUrl: v3.ClusterType})
		ads.RequestResponseAck(t, &discovery.DiscoveryRequest{TypeUrl: v3.ListenerType})
		ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
			TypeUrl:       v3.EndpointType,
			ResourceNames: []string{"outbound|9080||app2.default.svc.cluster.local"},
		})
		ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
			TypeUrl:       v3.RouteType,
			ResourceNames: []string{"80", "8080"},
		})

		node, _ := model.ParseServiceNodeWithMetadata(ads.ID, &model.NodeMetadata{})
		verifySyncStatus(t, s.Discovery, node.ID, true, true)
	})
	t.Run("sync status not set when Nackd", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		ads := s.ConnectADS()

		ads.RequestResponseNack(t, &discovery.DiscoveryRequest{TypeUrl: v3.ClusterType})
		ads.RequestResponseNack(t, &discovery.DiscoveryRequest{TypeUrl: v3.ListenerType})
		ads.RequestResponseNack(t, &discovery.DiscoveryRequest{
			TypeUrl:       v3.EndpointType,
			ResourceNames: []string{"outbound|9080||app2.default.svc.cluster.local"},
		})
		ads.RequestResponseNack(t, &discovery.DiscoveryRequest{
			TypeUrl:       v3.RouteType,
			ResourceNames: []string{"80", "8080"},
		})
		node, _ := model.ParseServiceNodeWithMetadata(ads.ID, &model.NodeMetadata{})
		verifySyncStatus(t, s.Discovery, node.ID, true, false)
	})
	t.Run("sync ecds", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
			ConfigString: mustReadFile(t, "./testdata/ecds.yaml"),
		})
		ads := s.ConnectADS()

		ads.RequestResponseNack(t, &discovery.DiscoveryRequest{
			TypeUrl:       v3.ExtensionConfigurationType,
			ResourceNames: []string{"extension-config"},
		})
		node, _ := model.ParseServiceNodeWithMetadata(ads.ID, &model.NodeMetadata{})
		verifySyncStatus(t, s.Discovery, node.ID, true, true)
	})
}

func getSyncStatus(t *testing.T, server *xds.DiscoveryServer) []xds.SyncStatus {
	req, err := http.NewRequest("GET", "/debug", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	syncz := http.HandlerFunc(server.Syncz)
	syncz.ServeHTTP(rr, req)
	got := []xds.SyncStatus{}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Error(err)
	}
	return got
}

func verifySyncStatus(t *testing.T, s *xds.DiscoveryServer, nodeID string, wantSent, wantAcked bool) {
	// This is a mostly horrible hack because the single pilot instance is shared across multiple tests
	// This makes this test contaminated by others and gives it horrible timing windows
	attempts := 5
	for i := 0; i < attempts; i++ {
		gotStatus := getSyncStatus(t, s)
		var errorHandler func(string, ...interface{})
		if i == attempts-1 {
			errorHandler = t.Errorf
		} else {
			errorHandler = t.Logf
		}
		for _, ss := range gotStatus {
			if ss.ProxyID == nodeID {
				if ss.ProxyVersion == "" {
					errorHandler("ProxyVersion should always be set for %v", nodeID)
				}
				if (ss.ClusterSent != "") != wantSent {
					errorHandler("wanted ClusterSent set %v got %v for %v", wantSent, ss.ClusterSent, nodeID)
				}
				if (ss.ClusterAcked != "") != wantAcked {
					errorHandler("wanted ClusterAcked set %v got %v for %v", wantAcked, ss.ClusterAcked, nodeID)
				}
				if (ss.ListenerSent != "") != wantSent {
					errorHandler("wanted ListenerSent set %v got %v for %v", wantSent, ss.ListenerSent, nodeID)
				}
				if (ss.ListenerAcked != "") != wantAcked {
					errorHandler("wanted ListenerAcked set %v got %v for %v", wantAcked, ss.ListenerAcked, nodeID)
				}
				if (ss.RouteSent != "") != wantSent {
					errorHandler("wanted RouteSent set %v got %v for %v", wantSent, ss.RouteSent, nodeID)
				}
				if (ss.RouteAcked != "") != wantAcked {
					errorHandler("wanted RouteAcked set %v got %v for %v", wantAcked, ss.RouteAcked, nodeID)
				}
				if (ss.EndpointSent != "") != wantSent {
					errorHandler("wanted EndpointSent set %v got %v for %v", wantSent, ss.EndpointSent, nodeID)
				}
				if (ss.EndpointAcked != "") != wantAcked {
					errorHandler("wanted EndpointAcked set %v got %v for %v", wantAcked, ss.EndpointAcked, nodeID)
				}
				if (ss.ExtensionConfigSent != "") != wantSent {
					errorHandler("wanted ExtesionConfigSent set %v got %v for %v", wantSent, ss.ExtensionConfigSent, nodeID)
				}
				if (ss.ExtensionConfigAcked != "") != wantAcked {
					errorHandler("wanted ExtensionConfigAcked set %v got %v for %v", wantAcked, ss.ExtensionConfigAcked, nodeID)
				}
				return
			}
		}
		errorHandler("node id %v not found", nodeID)
	}
}

func TestConfigDump(t *testing.T) {
	tests := []struct {
		name     string
		wantCode int
		proxyID  string
	}{
		{
			name:     "dumps most recent proxy with 200",
			proxyID:  "test.default",
			wantCode: 200,
		},
		{
			name:     "returns 404 if proxy not found",
			proxyID:  "not-found",
			wantCode: 404,
		},
		{
			name:     "returns 400 if no proxyID",
			proxyID:  "",
			wantCode: 400,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
			ads := s.ConnectADS()
			ads.RequestResponseAck(t, &discovery.DiscoveryRequest{TypeUrl: v3.ClusterType})
			ads.RequestResponseAck(t, &discovery.DiscoveryRequest{TypeUrl: v3.ListenerType})
			ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
				TypeUrl:       v3.RouteType,
				ResourceNames: []string{"80", "8080"},
			})

			wrapper := getConfigDump(t, s.Discovery, tt.proxyID, tt.wantCode)
			if wrapper != nil {
				if rs, err := wrapper.GetDynamicRouteDump(false); err != nil || len(rs.DynamicRouteConfigs) == 0 {
					t.Errorf("routes were present, must have received an older connection's dump, err: %v", err)
				}
			} else if tt.wantCode < 400 {
				t.Error("expected a non-nil wrapper with successful status code")
			}
		})
	}
}

func getConfigDump(t *testing.T, s *xds.DiscoveryServer, proxyID string, wantCode int) *configdump.Wrapper {
	path := "/config_dump"
	if proxyID != "" {
		path += fmt.Sprintf("?proxyID=%v", proxyID)
	}
	req, err := http.NewRequest("GET", path, nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	syncz := http.HandlerFunc(s.ConfigDump)
	syncz.ServeHTTP(rr, req)
	if rr.Code != wantCode {
		t.Errorf("wanted response code %v, got %v", wantCode, rr.Code)
	}
	if wantCode > 399 {
		return nil
	}
	got := &configdump.Wrapper{}
	if err := got.UnmarshalJSON(rr.Body.Bytes()); err != nil {
		t.Fatalf(err.Error())
	}
	return got
}

func TestDebugHandlers(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	req, err := http.NewRequest("GET", "/debug", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	debug := http.HandlerFunc(s.Discovery.Debug)
	debug.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Errorf("Error in generatating debug endpoint list")
	}
}
