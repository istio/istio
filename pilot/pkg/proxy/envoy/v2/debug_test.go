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
		_ = initLocalPilotTestEnv(t)
		adsstr := connectADS(t, util.MockPilotGrpcAddr)
		defer adsstr.CloseSend()
		sendEDSReq(t, []string{"service3.default.svc.cluster.local|http"}, app3Ip, adsstr)
		_, err := adsReceive(adsstr, 5*time.Second)
		if err != nil {
			t.Fatal("Recv failed", err)
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
		for i, ss := range got {
			if ss.ProxyID == "" {
				t.Errorf("%v sent not set", i)
			}
			if ss.Sent == "" {
				t.Errorf("%v sent not set", i)
			}
			if ss.Acked == "" {
				t.Errorf("%v acked not set", i)
			}
		}
	})
}
