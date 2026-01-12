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
	"reflect"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config/constants"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
)

func TestNDS(t *testing.T) {
	// The "auto allocate" test only needs a special case for the legacy auto allocation mode, so we disable the new one here
	// and only test the old one. The new one appears identically to manually-allocated SE from NDS perspective.
	test.SetForTest(t, &features.EnableIPAutoallocate, false)
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
						Ips:      []string{"240.240.116.21"},
						Registry: "External",
					},
					"random-2.host.example": {
						Ips:      []string{"9.9.9.9"},
						Registry: "External",
					},
					"random-3.host.example": {
						Ips:      []string{"240.240.81.100"},
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
				Node: &core.Node{
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
				t.Fatal("expected more than 0 entries in name table")
			}
			if diff := cmp.Diff(nt, tt.expected, protocmp.Transform()); diff != "" {
				t.Fatalf("name table does not match expected value:\n %v", diff)
			}
		})
	}
}

func TestGenerate(t *testing.T) {
	nt := &dnsProto.NameTable{
		Table: make(map[string]*dnsProto.NameTable_NameInfo),
	}
	emptyNameTable := model.Resources{&discovery.Resource{Resource: protoconv.MessageToAny(nt)}}

	cases := []struct {
		name      string
		proxy     *model.Proxy
		resources []string
		request   *model.PushRequest
		nameTable []*discovery.Resource
	}{
		{
			name:      "partial push with headless endpoint update",
			proxy:     &model.Proxy{Type: model.SidecarProxy},
			request:   &model.PushRequest{Reason: model.NewReasonStats(model.HeadlessEndpointUpdate), Forced: true},
			nameTable: emptyNameTable,
		},
		{
			name:      "full push",
			proxy:     &model.Proxy{Type: model.SidecarProxy},
			request:   &model.PushRequest{Full: true, Forced: true},
			nameTable: emptyNameTable,
		},
		{
			name:      "partial push with no headless endpoint update",
			proxy:     &model.Proxy{Type: model.SidecarProxy},
			request:   &model.PushRequest{Forced: true},
			nameTable: emptyNameTable,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.proxy.Metadata == nil {
				tt.proxy.Metadata = &model.NodeMetadata{}
			}
			tt.proxy.Metadata.ClusterID = constants.DefaultClusterName
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})

			gen := s.Discovery.Generators[v3.NameTableType]
			tt.request.Start = time.Now()
			nametable, _, _ := gen.Generate(s.SetupProxy(tt.proxy), &model.WatchedResource{ResourceNames: sets.New(tt.resources...)}, tt.request)
			if len(tt.nameTable) == 0 {
				if len(nametable) != 0 {
					t.Errorf("unexpected nametable. want: %v, got: %v", tt.nameTable, nametable)
				}
			} else {
				if !reflect.DeepEqual(tt.nameTable[0].Resource, nametable[0].Resource) {
					t.Errorf("unexpected nametable. want: %v, got: %v", tt.nameTable, nametable)
				}
			}
		})
	}
}
