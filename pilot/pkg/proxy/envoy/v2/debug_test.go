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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/tests/util"
)

func Test_Syncz(t *testing.T) {
	t.Run("return the sent and ack status of adsClient connections", func(t *testing.T) {
		initLocalPilotTestEnv(t)
		adsstr := connectADS(t, util.MockPilotGrpcAddr)
		defer adsstr.CloseSend()

		// Need to send two of each so that the second sends an Ack that is picked up
		sendEDSReq(t, []string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarId(app3Ip, "syncApp"), adsstr)
		sendEDSReq(t, []string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarId(app3Ip, "syncApp"), adsstr)
		sendCDSReq(t, sidecarId(app3Ip, "syncApp"), adsstr)
		sendCDSReq(t, sidecarId(app3Ip, "syncApp"), adsstr)
		sendLDSReq(t, sidecarId(app3Ip, "syncApp"), adsstr)
		sendLDSReq(t, sidecarId(app3Ip, "syncApp"), adsstr)
		sendRDSReq(t, sidecarId(app3Ip, "syncApp"), []string{"80", "8080"}, adsstr)
		sendRDSReq(t, sidecarId(app3Ip, "syncApp"), []string{"80", "8080"}, adsstr)
		for i := 0; i < 4; i++ {
			_, err := adsReceive(adsstr, 5*time.Second)
			if err != nil {
				t.Fatal("Recv failed", err)
			}
		}
		node, _ := model.ParseServiceNode(sidecarId(app3Ip, "syncApp"))
		verifySyncStatus(t, getSyncStatus(t), node.ID, true, true)
	})
	t.Run("sync status not set when Nackd", func(t *testing.T) {
		initLocalPilotTestEnv(t)
		adsstr := connectADS(t, util.MockPilotGrpcAddr)
		defer adsstr.CloseSend()

		sendEDSReq(t, []string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarId(app3Ip, "syncApp2"), adsstr)
		sendEDSNack(t, []string{"outbound|9080||app2.default.svc.cluster.local"}, sidecarId(app3Ip, "syncApp2"), adsstr)
		sendCDSReq(t, sidecarId(app3Ip, "syncApp2"), adsstr)
		sendCDSNack(t, sidecarId(app3Ip, "syncApp2"), adsstr)
		sendLDSReq(t, sidecarId(app3Ip, "syncApp2"), adsstr)
		sendLDSNack(t, sidecarId(app3Ip, "syncApp2"), adsstr)
		sendRDSReq(t, sidecarId(app3Ip, "syncApp2"), []string{"80", "8080"}, adsstr)
		sendRDSNack(t, sidecarId(app3Ip, "syncApp2"), []string{"80", "8080"}, adsstr)
		for i := 0; i < 5; i++ {
			_, err := adsReceive(adsstr, 5*time.Second)
			if err != nil {
				t.Fatal("Recv failed", err)
			}
		}
		node, _ := model.ParseServiceNode(sidecarId(app3Ip, "syncApp2"))
		verifySyncStatus(t, getSyncStatus(t), node.ID, true, false)
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

func verifySyncStatus(t *testing.T, gotStatus []v2.SyncStatus, nodeID string, wantSent, wantAcked bool) {
	// This is a mostly horrible hack because the single pilot instance is shared across multiple tests
	// This makes this test contaminated by others
	for _, ss := range gotStatus {
		if ss.ProxyID == nodeID {
			if (ss.ClusterSent != "") != wantSent {
				t.Errorf("wanted ClusterSent set %v got %v for %v", wantSent, ss.ClusterSent, nodeID)
			}
			if (ss.ClusterAcked != "") != wantAcked {
				t.Errorf("wanted ClusterAcked set %v got %v for %v", wantAcked, ss.ClusterAcked, nodeID)
			}
			if (ss.ListenerSent != "") != wantSent {
				t.Errorf("wanted ListenerSent set %v got %v for %v", wantSent, ss.ListenerSent, nodeID)
			}
			if (ss.ListenerAcked != "") != wantAcked {
				t.Errorf("wanted ListenerAcked set %v got %v for %v", wantAcked, ss.ListenerAcked, nodeID)
			}
			if (ss.RouteSent != "") != wantSent {
				t.Errorf("wanted RouteSent set %v got %v for %v", wantSent, ss.RouteSent, nodeID)
			}
			if (ss.RouteAcked != "") != wantAcked {
				t.Errorf("wanted RouteAcked set %v got %v for %v", wantAcked, ss.RouteAcked, nodeID)
			}
			if (ss.EndpointSent != "") != wantSent {
				t.Errorf("wanted EndpointSent set %v got %v for %v", wantSent, ss.EndpointSent, nodeID)
			}
			if (ss.EndpointAcked != "") != wantAcked {
				t.Errorf("wanted EndpointAcked set %v got %v for %v", wantAcked, ss.EndpointAcked, nodeID)
			}
			if (ss.EndpointPercent != 0) != wantAcked {
				t.Errorf("wanted EndpointPercent set %v got %v for %v", wantAcked, ss.EndpointPercent, nodeID)
			}
			return
		}
	}
	t.Errorf("node id %v not found", nodeID)
}
