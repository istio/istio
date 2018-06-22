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

	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/tests/util"
)

func Test_Syncz(t *testing.T) {
	t.Run("return the sent and ack status of adsClient connections", func(t *testing.T) {
		initLocalPilotTestEnv(t)
		adsstr := connectADS(t, util.MockPilotGrpcAddr)
		defer adsstr.CloseSend()
		sendCDSReq(t, sidecarId(app3Ip, "app3"), adsstr)
		sendLDSReq(t, sidecarId(app3Ip, "app3"), adsstr)
		sendRDSReq(t, sidecarId(app3Ip, "app3"), []string{"80", "8080"}, adsstr)
		sendEDSReq(t, []string{"outbound|9080||app2.default.svc.cluster.local"}, app3Ip, adsstr)
		for i := 0; i < 4; i++ {
			_, err := adsReceive(adsstr, 5*time.Second)
			if err != nil {
				t.Fatal("Recv failed", err)
			}
		}
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
		// This is a horrible hack because the single pilot instance is shared across multiple tests
		// As long as each field is set somewhere we don't actually care!
		checkMap := map[string]bool{
			"proxyID": false,
			"cSent":   false, "cAck": false,
			"lSent": false, "lAck": false,
			"rSent": false, "rAck": false,
			"eSent": false, "eAck": false, "ePercent": false,
		}
		for _, ss := range got {
			if ss.ProxyID != "" {
				checkMap["proxyID"] = true
			}
			if ss.ClusterSent != "" {
				checkMap["cSent"] = true
			}
			if ss.ClusterAcked != "" {
				checkMap["cAck"] = true
			}
			if ss.ListenerSent != "" {
				checkMap["lSent"] = true
			}
			if ss.ListenerAcked != "" {
				checkMap["lAck"] = true
			}
			if ss.RouteSent != "" {
				checkMap["rSent"] = true
			}
			if ss.RouteAcked != "" {
				checkMap["rAck"] = true
			}
			if ss.EndpointSent != "" {
				checkMap["eSent"] = true
			}
			if ss.EndpointAcked != "" {
				checkMap["eAck"] = true
			}
			if ss.EndpointPercent != 0 {
				checkMap["ePercent"] = true
			}
		}
		for field, present := range checkMap {
			if !present {
				t.Errorf("%v not set", field)
			}
		}
	})
}
