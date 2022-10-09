// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package xds_test

import (
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	dnsProto "istio.io/istio/pkg/dns/proto"
)

func TestNDS(t *testing.T) {
	cases := []struct {
		name     string
		meta     model.NodeMetadata
		expected *dnsProto.NameTable
	}{
		{
			name: "auto allocate",
			meta: model.NodeMetadata{
				DNSCapture:      true,
				DNSAutoAllocate: true,
			},
			expected: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
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
			},
		},
		{
			name: "just capture",
			meta: model.NodeMetadata{
				DNSCapture: true,
			},
			expected: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"random-2.host.example": {
						Ips:      []string{"9.9.9.9"},
						Registry: "External",
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				ConfigString: mustReadFile(t, "./testdata/nds-se.yaml"),
			})

			ads := s.ConnectADS().WithType(v3.NameTableType)
			res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
				Node: &corev3.Node{
					Id:       ads.ID,
					Metadata: tt.meta.ToStruct(),
				},
			})

			nt := &dnsProto.NameTable{}
			err := res.Resources[0].UnmarshalTo(nt)
			if err != nil {
				t.Fatal("Failed to unmarshal name table", err)
				return
			}
			if len(nt.Table) == 0 {
				t.Fatalf("expected more than 0 entries in name table")
			}
			if diff := cmp.Diff(nt, tt.expected, protocmp.Transform()); diff != "" {
				t.Fatalf("name table does not match expected value:\n %v", diff)
			}
		})
	}
}
