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
	"io/ioutil"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/tests/util"
)

// TestRDS is running RDSv2 tests.
func TestRDS(t *testing.T) {
	initLocalPilotTestEnv(t)

	t.Run("sidecar", func(t *testing.T) {
		rdsr, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		err = sendRDSReq(sidecarId(app3Ip, "app3"), []string{"80", "8080"}, "", rdsr)
		if err != nil {
			t.Fatal(err)
		}

		res, err := rdsr.Recv()
		if err != nil {
			t.Fatal("Failed to receive RDS", err)
		}

		strResponse, _ := model.ToJSONWithIndent(res, " ")
		_ = ioutil.WriteFile(util.IstioOut+"/rdsv2_sidecar.json", []byte(strResponse), 0644)

		if len(res.Resources) == 0 {
			t.Fatal("No response")
		}
	})

	t.Run("gateway", func(t *testing.T) {
		rdsr, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		err = sendRDSReq(gatewayId(gatewayIP), []string{"http.80", "https.443.https"}, "", rdsr)
		if err != nil {
			t.Fatal(err)
		}

		res, err := rdsr.Recv()
		if err != nil {
			t.Fatal("Failed to receive RDS", err)
			return
		}

		strResponse, _ := model.ToJSONWithIndent(res, " ")

		_ = ioutil.WriteFile(util.IstioOut+"/rdsv2_gateway.json", []byte(strResponse), 0644)

		if len(res.Resources) == 0 {
			t.Fatal("No response")
		}
	})

	// TODO: compare with some golden once it's stable
	// check that each mocked service and destination rule has a corresponding resource

	// TODO: dynamic checks ( see EDS )
}
