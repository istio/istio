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
	"testing"

	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

func TestCDS(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	adscon := s.ConnectADS()

	err := sendCDSReq(sidecarID(app3Ip, "app3"), adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adscon.Recv()
	if err != nil {
		t.Fatal("Failed to receive CDS", err)
		return
	}

	if len(res.Resources) == 0 {
		t.Fatal("No response")
	}
	if res.Resources[0].GetTypeUrl() != v3.ClusterType {
		t.Fatalf("Unexpected type url. want: %v, got: %v", v3.ClusterType, res.Resources[0].GetTypeUrl())
	}
}
