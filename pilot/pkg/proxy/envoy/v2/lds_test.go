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
	"io/ioutil"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/util"
)

// TestLDS using isolated namespaces
func TestLDSIsolated(t *testing.T) {

	_, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	// Sidecar in 'none' mode
	t.Run("sidecar_none", func(t *testing.T) {
		// TODO: add a Service with EDS resolution in the none ns.
		// The ServiceEntry only allows STATIC - both STATIC and EDS should generated TCP listeners on :port
		// while DNS and NONE should generate old-style bind ports.
		// Right now 'STATIC' and 'EDS' result in ClientSideLB in the internal object, so listener test is valid.

		ldsr, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
			Meta: map[string]string{
				model.NodeMetadataInterceptionMode: string(model.InterceptionNone),
			},
			IP:        "10.11.0.1", // matches none.yaml s1tcp.none
			Namespace: "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer ldsr.Close()

		ldsr.Watch()

		_, err = ldsr.Wait("rds", 50000*time.Second)
		if err != nil {
			t.Fatal("Failed to receive LDS", err)
			return
		}

		err = ldsr.Save(env.IstioOut + "/none")
		if err != nil {
			t.Fatal(err)
		}

		// s1http - inbound HTTP on 7071 (forwarding to app on 30000 + 7071 - or custom port)
		// All outbound on http proxy
		if len(ldsr.HTTPListeners) != 3 {
			// TODO: we are still debating if for HTTP services we have any use case to create a 127.0.0.1:port outbound
			// for the service (the http proxy is already covering this)
			t.Error("HTTP listeners, expecting 3 got ", len(ldsr.HTTPListeners), ldsr.HTTPListeners)
		}

		// s1tcp:2000 outbound, bind=true (to reach other instances of the service)
		// s1:5005 outbound, bind=true
		// :443 - https external, bind=false
		// 10.11.0.1_7070, bind=true -> inbound|2000|s1 - on port 7070, fwd to 37070
		// virtual
		if len(ldsr.TCPListeners) == 0 {
			t.Fatal("No response")
		}

		for _, s := range []string{"lds_tcp", "lds_http", "rds", "cds", "ecds"} {
			want, err := ioutil.ReadFile(env.IstioOut + "/none_" + s + ".json")
			if err != nil {
				t.Fatal(err)
			}
			got, err := ioutil.ReadFile("testdata/none_" + s + ".json")
			if err != nil {
				t.Fatal(err)
			}

			if err = util.Compare(got, want); err != nil {
				t.Error(fmt.Errorf("error in golden file %s %v", s, err))
			}
		}

		// TODO: check bind==true
		// TODO: verify listeners for outbound are on 127.0.0.1 (not yet), port 2000, 2005, 2007
		// TODO: verify virtual listeners for unsupported cases
		// TODO: add and verify SNI listener on 127.0.0.1:443
		// TODO: verify inbound service port is on 127.0.0.1, and containerPort on 0.0.0.0
		// TODO: BUG, SE with empty endpoints is rejected - it is actually valid config (service may not have endpoints)
	})

	// Test for the examples in the ServiceEntry doc
	t.Run("se_example", func(t *testing.T) {
		// TODO: add a Service with EDS resolution in the none ns.
		// The ServiceEntry only allows STATIC - both STATIC and EDS should generated TCP listeners on :port
		// while DNS and NONE should generate old-style bind ports.
		// Right now 'STATIC' and 'EDS' result in ClientSideLB in the internal object, so listener test is valid.

		ldsr, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
			Meta:      map[string]string{},
			IP:        "10.12.0.1", // matches none.yaml s1tcp.none
			Namespace: "seexamples",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer ldsr.Close()

		ldsr.Watch()

		_, err = ldsr.Wait("rds", 50000*time.Second)
		if err != nil {
			t.Fatal("Failed to receive LDS", err)
			return
		}

		err = ldsr.Save(env.IstioOut + "/seexample")
		if err != nil {
			t.Fatal(err)
		}
	})

	// Test for the examples in the ServiceEntry doc
	t.Run("se_examplegw", func(t *testing.T) {
		// TODO: add a Service with EDS resolution in the none ns.
		// The ServiceEntry only allows STATIC - both STATIC and EDS should generated TCP listeners on :port
		// while DNS and NONE should generate old-style bind ports.
		// Right now 'STATIC' and 'EDS' result in ClientSideLB in the internal object, so listener test is valid.

		ldsr, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
			Meta:      map[string]string{},
			IP:        "10.13.0.1",
			Namespace: "exampleegressgw",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer ldsr.Close()

		ldsr.Watch()

		_, err = ldsr.Wait("rds", 50000*time.Second)
		if err != nil {
			t.Fatal("Failed to receive LDS", err)
			return
		}

		err = ldsr.Save(env.IstioOut + "/seexample-eg")
		if err != nil {
			t.Fatal(err)
		}
	})

}

// TestLDS is running LDSv2 tests.
func TestLDS(t *testing.T) {
	_, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	t.Run("sidecar", func(t *testing.T) {
		ldsr, cancel, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()
		err = sendLDSReq(sidecarID(app3Ip, "app3"), ldsr)
		if err != nil {
			t.Fatal(err)
		}

		res, err := ldsr.Recv()
		if err != nil {
			t.Fatal("Failed to receive LDS", err)
			return
		}

		strResponse, _ := model.ToJSONWithIndent(res, " ")
		_ = ioutil.WriteFile(env.IstioOut+"/ldsv2_sidecar.json", []byte(strResponse), 0644)

		if len(res.Resources) == 0 {
			t.Fatal("No response")
		}
	})

	// 'router' or 'gateway' type of listener
	t.Run("gateway", func(t *testing.T) {
		ldsr, cancel, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()
		err = sendLDSReq(gatewayID(gatewayIP), ldsr)
		if err != nil {
			t.Fatal(err)
		}

		res, err := ldsr.Recv()
		if err != nil {
			t.Fatal("Failed to receive LDS", err)
		}

		strResponse, _ := model.ToJSONWithIndent(res, " ")

		_ = ioutil.WriteFile(env.IstioOut+"/ldsv2_gateway.json", []byte(strResponse), 0644)

		if len(res.Resources) == 0 {
			t.Fatal("No response")
		}
	})

	t.Run("ingress", func(t *testing.T) {
		ldsr, cancel, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()

		err = sendLDSReq(ingressID(ingressIP), ldsr)
		if err != nil {
			t.Fatal(err)
		}

		res, err := ldsr.Recv()
		if err != nil {
			t.Fatal("Failed to receive LDS", err)
			return
		}

		strResponse, _ := model.ToJSONWithIndent(res, " ")

		_ = ioutil.WriteFile(env.IstioOut+"/ads_lds_ingress.json", []byte(strResponse), 0644)

		if len(res.Resources) == 0 {
			t.Fatal("No response")
		}
	})
	// TODO: compare with some golden once it's stable
	// check that each mocked service and destination rule has a corresponding resource

	// TODO: dynamic checks ( see EDS )
}

// TODO: helper to test the http listener content
// - file access log
// - generate request id
// - cors, fault, router filters
// - tracing
//
