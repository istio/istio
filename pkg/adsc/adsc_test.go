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

package adsc

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/testing/protocmp"
	anypb "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"istio.io/api/label"
	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/schema/collections"
)

type testAdscRunServer struct{}

var StreamHandler func(stream xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error

func (t *testAdscRunServer) StreamAggregatedResources(stream xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	return StreamHandler(stream)
}

func (t *testAdscRunServer) DeltaAggregatedResources(xdsapi.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return nil
}

func TestADSC_Run(t *testing.T) {
	type testCase struct {
		desc                 string
		inAdsc               *ADSC
		streamHandler        func(server xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error
		expectedADSResources *ADSC
		validator            func(testCase) error
	}
	var tests []testCase

	type testDesc struct {
		desc             string
		reqTypeUrls      []string
		expectedTypeUrls []string // nil means equals to requested
		validator        func(testCase) error
	}

	descs := []testDesc{
		{
			desc:        "stream-no-resources",
			reqTypeUrls: []string{},
		},
		{
			desc:        "stream-2-unnamed-resources",
			reqTypeUrls: []string{"foo", "bar"},
		},
		// todo tests for listeners, clusters, eds, and routes, not sure how to do this.
	}

	initTypeUrls := func() []string {
		var ret []string
		for _, req := range ConfigInitialRequests() {
			ret = append(ret, req.TypeUrl)
		}
		return ret
	}()
	incompleteTypeUrls := func() []string {
		var ret []string
		for idx, item := range initTypeUrls {
			if strings.Count(item, "/") == 3 {
				ret = append(ret, initTypeUrls[:idx]...)
				ret = append(ret, initTypeUrls[idx+1:]...)
				break
			}
		}
		if ret == nil {
			ret = initTypeUrls
		}
		return ret
	}()
	descs = append(descs, testDesc{
		desc:        "mcp-should-hasSynced",
		reqTypeUrls: initTypeUrls,
		validator: func(tc testCase) error {
			if !tc.inAdsc.HasSynced() {
				return fmt.Errorf("adsc not synced")
			}
			return nil
		},
	})
	if len(incompleteTypeUrls) != len(initTypeUrls) {
		descs = append(descs, testDesc{
			desc:             "mcp-should-not-hasSynced",
			reqTypeUrls:      initTypeUrls,
			expectedTypeUrls: incompleteTypeUrls,
			validator: func(tc testCase) error {
				if tc.inAdsc.HasSynced() {
					return fmt.Errorf("adsc synced but should not")
				}
				return nil
			},
		})
	}

	for _, item := range descs {
		desc := item // avoid refer to on-stack-var
		expected := map[string]*xdsapi.DiscoveryResponse{}
		if desc.expectedTypeUrls == nil {
			desc.expectedTypeUrls = desc.reqTypeUrls
		}
		var initReqs []*xdsapi.DiscoveryRequest
		for _, typeURL := range desc.reqTypeUrls {
			initReqs = append(initReqs, &xdsapi.DiscoveryRequest{TypeUrl: typeURL})
		}
		for _, typeURL := range desc.expectedTypeUrls {
			expected[typeURL] = &xdsapi.DiscoveryResponse{TypeUrl: typeURL}
		}

		if desc.validator == nil {
			desc.validator = func(tc testCase) error {
				if !cmp.Equal(tc.inAdsc.Received, tc.expectedADSResources.Received, protocmp.Transform()) {
					return fmt.Errorf("%s: expected recv %v got %v", tc.desc, tc.expectedADSResources.Received, tc.inAdsc.Received)
				}
				return nil
			}
		}

		tc := testCase{
			desc: desc.desc,
			inAdsc: &ADSC{
				Received:   make(map[string]*xdsapi.DiscoveryResponse),
				Updates:    make(chan string),
				XDSUpdates: make(chan *xdsapi.DiscoveryResponse),
				RecvWg:     sync.WaitGroup{},
				cfg: &Config{
					InitialDiscoveryRequests: initReqs,
				},
				VersionInfo: map[string]string{},
				sync:        map[string]time.Time{},
			},
			streamHandler: func(stream xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
				for _, typeURL := range desc.expectedTypeUrls {
					_ = stream.Send(&xdsapi.DiscoveryResponse{
						TypeUrl: typeURL,
					})
				}
				return nil
			},
			expectedADSResources: &ADSC{
				Received: expected,
			},
			validator: desc.validator,
		}

		tests = append(tests, tc)
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			StreamHandler = tt.streamHandler
			l, err := net.Listen("tcp", ":0")
			if err != nil {
				t.Errorf("Unable to listen with tcp err %v", err)
				return
			}
			tt.inAdsc.url = l.Addr().String()
			xds := grpc.NewServer()
			xdsapi.RegisterAggregatedDiscoveryServiceServer(xds, new(testAdscRunServer))
			go func() {
				err = xds.Serve(l)
				if err != nil {
					log.Println(err)
				}
			}()
			defer xds.GracefulStop()
			if err != nil {
				t.Errorf("Could not start serving ads server %v", err)
				return
			}

			if err := tt.inAdsc.Dial(); err != nil {
				t.Errorf("Dial error: %v", err)
				return
			}
			if err := tt.inAdsc.Run(); err != nil {
				t.Errorf("ADSC: failed running %v", err)
				return
			}
			tt.inAdsc.RecvWg.Wait()

			if err := tt.validator(tt); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestADSC_Save(t *testing.T) {
	tests := []struct {
		desc         string
		expectedJSON map[string]string
		adsc         *ADSC
		err          error
	}{
		{
			desc: "empty",
			expectedJSON: map[string]string{
				"_lds_tcp":  `[]`,
				"_lds_http": `[]`,
				"_rds":      `[]`,
				"_eds":      `[]`,
				"_ecds":     `[]`,
				"_cds":      `[]`,
			},
			err: nil,
			adsc: &ADSC{
				tcpListeners:  map[string]*listener.Listener{},
				httpListeners: map[string]*listener.Listener{},
				routes:        map[string]*route.RouteConfiguration{},
				edsClusters:   map[string]*cluster.Cluster{},
				clusters:      map[string]*cluster.Cluster{},
				eds:           map[string]*endpoint.ClusterLoadAssignment{},
			},
		},
		{
			desc: "populated",
			err:  nil,
			expectedJSON: map[string]string{
				"_lds_tcp": `[
  {
    "listener-1": {
      "name": "bar"
    }
  },
  {
    "listener-2": {
      "name": "mar"
    }
  }
]`,
				"_lds_http": `[
  {
    "http-list-1": {
      "name": "bar"
    }
  },
  {
    "http-list-2": {
      "name": "mar"
    }
  }
]`,
				"_rds": `[
  {
    "route-1": {
      "name": "mar"
    }
  }
]`,
				"_eds": `[
  {
    "load-assignment-1": {
      "clusterName": "foo"
    }
  }
]`,
				"_ecds": `[
  {
    "eds-cluster-1": {
      "name": "test"
    }
  }
]`,
				"_cds": `[
  {
    "cluster-1": {
      "name": "foo"
    }
  }
]`,
			},
			adsc: &ADSC{
				tcpListeners: map[string]*listener.Listener{
					"listener-1": {
						Name: "bar",
					},
					"listener-2": {
						Name: "mar",
					},
				},
				httpListeners: map[string]*listener.Listener{
					"http-list-1": {
						Name: "bar",
					},
					"http-list-2": {
						Name: "mar",
					},
				},
				routes: map[string]*route.RouteConfiguration{
					"route-1": {
						Name: "mar",
					},
				},
				edsClusters: map[string]*cluster.Cluster{
					"eds-cluster-1": {
						Name: "test",
					},
				},
				clusters: map[string]*cluster.Cluster{
					"cluster-1": {
						Name: "foo",
					},
				},
				eds: map[string]*endpoint.ClusterLoadAssignment{
					"load-assignment-1": {
						ClusterName: "foo",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			base := t.TempDir()
			if err := tt.adsc.Save(base); (err == nil && tt.err != nil) || (err != nil && tt.err == nil) {
				t.Errorf("AdscSave() => %v expected err %v", err, tt.err)
			}
			if ldsTCP := readFile(base+"_lds_tcp.json", t); ldsTCP != tt.expectedJSON["_lds_tcp"] {
				t.Errorf("AdscSave() => %s expected ldsTcp %s\n%v", ldsTCP, tt.expectedJSON["_lds_tcp"], cmp.Diff(ldsTCP, tt.expectedJSON["_lds_tcp"]))
			}
			if ldsHTTP := readFile(base+"_lds_http.json", t); ldsHTTP != tt.expectedJSON["_lds_http"] {
				t.Errorf("AdscSave() => %s expected ldsHttp %s", ldsHTTP, tt.expectedJSON["_lds_http"])
			}
			if rds := readFile(base+"_rds.json", t); rds != tt.expectedJSON["_rds"] {
				t.Errorf("AdscSave() => %s expected rds %s", rds, tt.expectedJSON["_rds"])
			}
			if ecds := readFile(base+"_ecds.json", t); ecds != tt.expectedJSON["_ecds"] {
				t.Errorf("AdscSave() => %s expected ecds %s", ecds, tt.expectedJSON["_ecds"])
			}
			if cds := readFile(base+"_cds.json", t); cds != tt.expectedJSON["_cds"] {
				t.Errorf("AdscSave() => %s expected cds %s", cds, tt.expectedJSON["_cds"])
			}
			if eds := readFile(base+"_eds.json", t); eds != tt.expectedJSON["_eds"] {
				t.Errorf("AdscSave() => %s expected eds %s", eds, tt.expectedJSON["_eds"])
			}
			saveTeardown(base, t)
		})
	}
}

func saveTeardown(base string, t *testing.T) {
	if err := os.Remove(base + "_lds_tcp.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
	if err := os.Remove(base + "_lds_http.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
	if err := os.Remove(base + "_cds.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
	if err := os.Remove(base + "_rds.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
	if err := os.Remove(base + "_ecds.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
	if err := os.Remove(base + "_eds.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
}

func readFile(dir string, t *testing.T) string {
	dat, err := os.ReadFile(dir)
	if err != nil {
		t.Fatalf("file %s issue: %v", dat, err)
	}
	return string(dat)
}

func TestADSC_handleMCP(t *testing.T) {
	rev := "test-rev"
	adsc := &ADSC{
		VersionInfo: map[string]string{},
		Store:       model.MakeIstioStore(memory.Make(collections.Pilot)),
		cfg:         &Config{Revision: rev},
	}

	patchLabel := func(lbls map[string]string, name, value string) map[string]string {
		if lbls == nil {
			lbls = map[string]string{}
		}
		lbls[name] = value
		return lbls
	}

	tests := []struct {
		desc              string
		resources         []*anypb.Any
		expectedResources [][]string
	}{
		{
			desc: "create-resources",
			resources: []*anypb.Any{
				constructResource("foo1", "foo1.bar.com", "192.1.1.1", "1"),
				constructResource("foo2", "foo2.bar.com", "192.1.1.2", "1"),
			},
			expectedResources: [][]string{
				{"foo1", "foo1.bar.com", "192.1.1.1"},
				{"foo2", "foo2.bar.com", "192.1.1.2"},
			},
		},
		{
			desc: "create-resources-rev-1",
			resources: []*anypb.Any{
				constructResource("foo1", "foo1.bar.com", "192.1.1.1", "1"),
				constructResourceWithOptions("foo2", "foo2.bar.com", "192.1.1.2", "1", func(resource *mcp.Resource) {
					resource.Metadata.Labels = patchLabel(resource.Metadata.Labels, label.IoIstioRev.Name, rev+"wrong") // to del
				}),
				constructResourceWithOptions("foo3", "foo3.bar.com", "192.1.1.3", "1", func(resource *mcp.Resource) {
					resource.Metadata.Labels = patchLabel(resource.Metadata.Labels, label.IoIstioRev.Name, rev) // to add
				}),
			},
			expectedResources: [][]string{
				{"foo1", "foo1.bar.com", "192.1.1.1"},
				{"foo3", "foo3.bar.com", "192.1.1.3"},
			},
		},
		{
			desc: "create-resources-rev-2",
			resources: []*anypb.Any{
				constructResource("foo1", "foo1.bar.com", "192.1.1.1", "1"),
				constructResourceWithOptions("foo2", "foo2.bar.com", "192.1.1.2", "1", func(resource *mcp.Resource) {
					resource.Metadata.Labels = patchLabel(resource.Metadata.Labels, label.IoIstioRev.Name, rev) // to add back
				}),
				constructResourceWithOptions("foo3", "foo3.bar.com", "192.1.1.3", "1", func(resource *mcp.Resource) {
					resource.Metadata.Labels = patchLabel(resource.Metadata.Labels, label.IoIstioRev.Name, rev+"wrong") // to del
				}),
			},
			expectedResources: [][]string{
				{"foo1", "foo1.bar.com", "192.1.1.1"},
				{"foo2", "foo2.bar.com", "192.1.1.2"},
			},
		},
		{
			desc: "update-and-create-resources",
			resources: []*anypb.Any{
				constructResource("foo1", "foo1.bar.com", "192.1.1.11", "2"),
				constructResource("foo2", "foo2.bar.com", "192.1.1.22", "1"),
				constructResource("foo3", "foo3.bar.com", "192.1.1.3", ""),
			},
			expectedResources: [][]string{
				{"foo1", "foo1.bar.com", "192.1.1.11"},
				{"foo2", "foo2.bar.com", "192.1.1.2"},
				{"foo3", "foo3.bar.com", "192.1.1.3"},
			},
		},
		{
			desc: "update-delete-and-create-resources",
			resources: []*anypb.Any{
				constructResource("foo2", "foo2.bar.com", "192.1.1.222", "4"),
				constructResource("foo4", "foo4.bar.com", "192.1.1.4", "1"),
			},
			expectedResources: [][]string{
				{"foo2", "foo2.bar.com", "192.1.1.222"},
				{"foo4", "foo4.bar.com", "192.1.1.4"},
			},
		},
		{
			desc: "update-and-delete-resources",
			resources: []*anypb.Any{
				constructResource("foo2", "foo2.bar.com", "192.2.2.22", "3"),
				constructResource("foo3", "foo3.bar.com", "192.1.1.33", ""),
			},
			expectedResources: [][]string{
				{"foo2", "foo2.bar.com", "192.2.2.22"},
				{"foo3", "foo3.bar.com", "192.1.1.33"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			gvk := []string{"networking.istio.io", "v1alpha3", "ServiceEntry"}
			adsc.handleMCP(gvk, tt.resources)
			configs, _ := adsc.Store.List(collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(), "")
			if len(configs) != len(tt.expectedResources) {
				t.Errorf("expected %v got %v", len(tt.expectedResources), len(configs))
			}
			configMap := make(map[string][]string)
			for _, conf := range configs {
				service, _ := conf.Spec.(*networking.ServiceEntry)
				configMap[conf.Name] = []string{conf.Name, service.Hosts[0], service.Addresses[0]}
			}
			for _, expected := range tt.expectedResources {
				got, ok := configMap[expected[0]]
				if !ok {
					t.Errorf("expected %v got none", expected)
				} else {
					for i, value := range expected {
						if value != got[i] {
							t.Errorf("expected %v got %v", value, got[i])
						}
					}
				}
			}
		})
	}
}

func constructResourceWithOptions(name string, host string, address, version string, options ...func(resource *mcp.Resource)) *anypb.Any {
	service := &networking.ServiceEntry{
		Hosts:     []string{host},
		Addresses: []string{address},
	}
	seAny := protoconv.MessageToAny(service)
	resource := &mcp.Resource{
		Metadata: &mcp.Metadata{
			Name:       "default/" + name,
			CreateTime: timestamppb.Now(),
			Version:    version,
		},
		Body: seAny,
	}

	for _, o := range options {
		o(resource)
	}

	resAny := protoconv.MessageToAny(resource)
	return &anypb.Any{
		TypeUrl: resAny.TypeUrl,
		Value:   resAny.Value,
	}
}

func constructResource(name string, host string, address, version string) *anypb.Any {
	return constructResourceWithOptions(name, host, address, version)
}
