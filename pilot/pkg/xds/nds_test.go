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

	"github.com/golang/protobuf/ptypes"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	nds "istio.io/istio/pilot/pkg/proto"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

func TestNDS(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadFile(t, "./testdata/nds-se.yaml"),
	})

	adscon := s.ConnectADS()

	err := sendNDSReq(sidecarID(app3Ip, "app3"), "ns2", adscon)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adscon.Recv()
	if err != nil {
		t.Fatal("Failed to receive NDS", err)
		return
	}

	if len(res.Resources) == 0 {
		t.Fatal("No response")
	}
	if res.Resources[0].GetTypeUrl() != v3.NameTableType {
		t.Fatalf("Unexpected type url. want: %v, got: %v", v3.NameTableType, res.Resources[0].GetTypeUrl())
	}
	var nt nds.NameTable
	err = ptypes.UnmarshalAny(res.Resources[0], &nt)
	if err != nil {
		t.Fatal("Failed to unmarshall name table", err)
		return
	}
	if len(nt.Table) == 0 {
		t.Fatalf("expected more than 0 entries in name table")
	}
	expectedNameTable := &nds.NameTable{
		Table: map[string]*nds.NameTable_NameInfo{
			"random-1.host.example": {
				Ips:      []string{"240.240.0.1"},
				Registry: "External",
			},
			"random-2.host.example": {
				Ips:      []string{"9.9.9.9"},
				Registry: "External",
			},
			"random-3.host.example": {
				Ips:      []string{"240.240.0.2"},
				Registry: "External",
			},
		},
	}
	if diff := cmp.Diff(nt, expectedNameTable, protocmp.Transform()); diff != "" {
		t.Fatalf("name table does not match expected value:\n %v", diff)
	}
}
