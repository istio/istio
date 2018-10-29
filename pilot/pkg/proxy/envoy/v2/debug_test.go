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

package v2_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/tests/util"
)

func TestSyncz(t *testing.T) {
	t.Run("return the sent and ack status of adsClient connections", func(t *testing.T) {
		initLocalPilotTestEnv(t)
		adsstr, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer adsstr.CloseSend()

		// Need to send two of each so that the second sends an Ack that is picked up
		if err := sendEDSReq([]string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarId(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendEDSReq([]string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarId(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendCDSReq(sidecarId(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendCDSReq(sidecarId(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendLDSReq(sidecarId(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendLDSReq(sidecarId(app3Ip, "syncApp"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendRDSReq(sidecarId(app3Ip, "syncApp"), []string{"80", "8080"}, adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendRDSReq(sidecarId(app3Ip, "syncApp"), []string{"80", "8080"}, adsstr); err != nil {
			t.Fatal(err)
		}

		for i := 0; i < 4; i++ {
			_, err := adsReceive(adsstr, 5*time.Second)
			if err != nil {
				t.Fatal("Recv failed", err)
			}
		}
		node, _ := model.ParseServiceNode(sidecarId(app3Ip, "syncApp"))
		verifySyncStatus(t, node.ID, true, true)
	})
	t.Run("sync status not set when Nackd", func(t *testing.T) {
		initLocalPilotTestEnv(t)
		adsstr, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer adsstr.CloseSend()

		if err := sendEDSReq([]string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarId(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendEDSNack([]string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarId(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendCDSReq(sidecarId(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendCDSNack(sidecarId(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendLDSReq(sidecarId(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendLDSNack(sidecarId(app3Ip, "syncApp2"), adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendRDSReq(sidecarId(app3Ip, "syncApp2"), []string{"80", "8080"}, adsstr); err != nil {
			t.Fatal(err)
		}
		if err := sendRDSNack(sidecarId(app3Ip, "syncApp2"), []string{"80", "8080"}, adsstr); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			_, err := adsReceive(adsstr, 5*time.Second)
			if err != nil {
				t.Fatal("Recv failed", err)
			}
		}
		node, _ := model.ParseServiceNode(sidecarId(app3Ip, "syncApp2"))
		verifySyncStatus(t, node.ID, true, false)
	})
}

func getSyncStatus(t *testing.T) []v2.SyncStatus {
	req, err := http.NewRequest("GET", "/debug", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	syncz := http.HandlerFunc(v2.Syncz)
	syncz.ServeHTTP(rr, req)
	got := []v2.SyncStatus{}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Error(err)
	}
	return got
}

func verifySyncStatus(t *testing.T, nodeID string, wantSent, wantAcked bool) {
	// This is a mostly horrible hack because the single pilot instance is shared across multiple tests
	// This makes this test contaminated by others and gives it horrible timing windows
	attempts := 5
	for i := 0; i < attempts; i++ {
		gotStatus := getSyncStatus(t)
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
				if (ss.EndpointPercent != 0) != wantAcked {
					errorHandler("wanted EndpointPercent set %v got %v for %v", wantAcked, ss.EndpointPercent, nodeID)
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
			proxyID:  "dumpApp-644fc65469-96dza.testns",
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
			s := initLocalPilotTestEnv(t)
			for i := 0; i < 2; i++ {
				envoy, err := connectADS(util.MockPilotGrpcAddr)
				if err != nil {
					t.Fatal(err)
				}
				defer envoy.CloseSend()
				if err := sendCDSReq(sidecarId(app3Ip, "dumpApp"), envoy); err != nil {
					t.Fatal(err)
				}
				if err := sendLDSReq(sidecarId(app3Ip, "dumpApp"), envoy); err != nil {
					t.Fatal(err)
				}
				// Only most recent proxy will have routes
				if i == 1 {
					if err := sendRDSReq(sidecarId(app3Ip, "dumpApp"), []string{"80", "8080"}, envoy); err != nil {
						t.Fatal(err)
					}
					_, err := adsReceive(envoy, 5*time.Second)
					if err != nil {
						t.Fatal("Recv failed", err)
					}
				}
				for j := 0; j < 2; j++ {
					_, err := adsReceive(envoy, 5*time.Second)
					if err != nil {
						t.Fatal("Recv failed", err)
					}
				}
			}
			wrapper := getConfigDump(t, s.EnvoyXdsServer, tt.proxyID, tt.wantCode)
			if wrapper != nil {
				if rs, err := wrapper.GetDynamicRouteDump(false); err != nil || len(rs.DynamicRouteConfigs) == 0 {
					t.Errorf("routes were present, must have received an older connection's dump")
				}
			} else if tt.wantCode < 400 {
				t.Error("expected a non-nil wrapper with successful status code")
			}
		})
	}
}

func getConfigDump(t *testing.T, s *v2.DiscoveryServer, proxyID string, wantCode int) *configdump.Wrapper {
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
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf(err.Error())
	}
	return got
}
