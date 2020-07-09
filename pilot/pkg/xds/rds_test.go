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
	"fmt"
	"io/ioutil"
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/istio/tests/util"
)

// TestRDS is running RDSv2 tests.
func TestRDS(t *testing.T) {
	_, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	tests := []struct {
		name   string
		node   string
		routes []string
	}{
		{
			"sidecar_new",
			sidecarID(app3Ip, "app3"),
			[]string{"80", "8080"},
		},
		{
			"gateway_new",
			gatewayID(gatewayIP),
			[]string{"http.80", "https.443.https.my-gateway.testns"},
		},
		{
			// Even if we get a bad route, we should still send Envoy an empty response, rather than
			// ignore it. If we ignore the route, the listeners can get stuck waiting forever.
			"sidecar_badroute",
			sidecarID(app3Ip, "app3"),
			[]string{"ht&p"},
		},
	}

	for idx, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rdsr, cancel, err := connectADS(util.MockPilotGrpcAddr)
			if err != nil {
				t.Fatal(err)
			}
			defer cancel()

			err = sendRDSReq(tt.node, tt.routes, "", "", rdsr)
			if err != nil {
				t.Fatal(err)
			}

			res, err := rdsr.Recv()
			if err != nil {
				t.Fatal("Failed to receive RDS", err)
			}

			strResponse, _ := gogoprotomarshal.ToJSONWithIndent(res, " ")
			_ = ioutil.WriteFile(env.IstioOut+fmt.Sprintf("/rdsv2/%s_%d.json", tt.name, idx), []byte(strResponse), 0644)
			if len(res.Resources) == 0 {
				t.Fatal("No response")
			}
		})
	}
}
