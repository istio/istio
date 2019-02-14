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
	"fmt"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/tests/util"
)

type test struct {
	name     string
	node     string
	routes   []string
	response bool
}

var tt = test{
	"sidecar_new",
	sidecarID(app3Ip, "app3"),
	[]string{"80", "8080"},
	true,
}

// TestRDS is running RDSv2 tests.
func TestVHDS(t *testing.T) {

	//subscribe
	//unsubscribe
	//route not subscribed to
	//using default sidecar
	//host does not exist
	//namespace does not exist
	//app is not http
	//sidecar previously defined
	//future rds requests don't return vhosts after initial vhds
	//wildcard domain
	//multiple endpoints
	// no custom egress listeners defined... by default, the sidecar includes all domains, but has a nil sidecar cfg. I.e. uninitialized. Test it
	//more than one egresslistener -- is this valid? It's an array...
	//unsolicited updates

	//BAVERY_TODO: Don't break mixer -- add entries if required

	_, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	t.Run("vhds_before_rds", func(t *testing.T) {
		adsConnection, cancel, err := connectDeltaADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()

		err = sendDeltaVHDSSubscribeReq(sidecarID(app3Ip, "app3"), []string{"80$appendh.test.istio.io"}, "", adsConnection, false)
		if err != nil {
			t.Fatal(err)
		}

		res, err := adsConnection.Recv()
		if err == nil {
			t.Fatal("Received VHDS when it shouldn't be enabled")
		}

		strResponse, _ := model.ToJSONWithIndent(res, " ")
		fmt.Printf("Response: %+v", strResponse)
	})

	t.Run("host_with_no_route", func(t *testing.T) {
		adsConnection, cancel, err := connectDeltaADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()

		err = sendDeltaVHDSSubscribeReq(sidecarID(app3Ip, "app3"), []string{"host_with_no_route"}, "", adsConnection, true)
		if err != nil {
			t.Fatal(err)
		}

		res, err := adsConnection.Recv()
		if err != nil {
			t.Fatal("Failed to receive VHDS", err)
		}

		if len(res.Resources) != 0 {
			t.Fatal("Received resources when not expected.")
		}

		strResponse, _ := model.ToJSONWithIndent(res, " ")
		fmt.Printf("Response: %+v", strResponse)
	})

	t.Run("send_rds_vhds_disabled", func(t *testing.T) {

	})

	t.Run("send_rds_vhds_enabled", func(t *testing.T) {

	})

	t.Run("multiple_vhds_different_routes", func(t *testing.T) {

	})

	t.Run("multiple_vhds_same_route", func(t *testing.T) {

	})

	t.Run("rds_vhds_disabled", func(t *testing.T) {
		adsConnection, cancel, err := connectDeltaADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()

		//Send on-demand RDS request
		err = sendDeltaRDSSubscribeReq(tt.node, tt.routes, "", adsConnection, false)
		if err != nil {
			t.Fatal(err)
		}

		routeResponse, err := adsConnection.Recv()
		if err != nil {
			t.Fatal("Failed to receive RDS", err)
		}

		routeConfig, _ := unmarshallRoute(routeResponse.Resources[0].Resource.Value)
		if len(routeConfig.VirtualHosts) == 0 {
			t.Fatal("VHDS is disabled and no virtualhosts were received.")
		}
	})

	t.Run("vhds_after_rds", func(t *testing.T) {
		adsConnection, _, err := connectDeltaADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}

		//Send on-demand RDS request
		err = sendDeltaRDSSubscribeReq(tt.node, tt.routes, "", adsConnection, true)
		if err != nil {
			t.Fatal(err)
		}

		routeResponse, err := adsConnection.Recv()
		if err != nil {
			t.Fatal("Failed to receive RDS", err)
		}

		routeConfig, _ := unmarshallRoute(routeResponse.Resources[0].Resource.Value)
		if len(routeConfig.VirtualHosts) != 0 {
			t.Fatal("VHDS is enabled, but virtualhosts were received over RDS.")
		}

		err = sendDeltaVHDSSubscribeReq(sidecarID(app3Ip, "app3"), []string{"80$appendh.test.istio.io"}, "", adsConnection, true)
		if err != nil {
			t.Fatal(err)
		}

		vhdsResponse, err := adsConnection.Recv()
		if err != nil {
			t.Fatal("Failed to receive VHDS", err)
		}

		if len(vhdsResponse.Resources) == 0 {
			t.Fatal("No virtual hosts received over VHDS.")
		}

		vhostJson, _ := model.ToJSONWithIndent(vhdsResponse, " ")
		fmt.Printf("Bavery:Response3: %+v", vhostJson)
	})
}
