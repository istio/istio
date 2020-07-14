// Copyright 2020 Istio Authors
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
	"io/ioutil"
	"log"
	"net"
	"os"
	"sync"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/testing/protocmp"
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
	tests := []struct {
		desc                 string
		inAdsc               *ADSC
		port                 uint32
		streamHandler        func(server xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error
		expectedADSResources *ADSC
	}{
		{
			desc: "stream-no-resources",
			inAdsc: &ADSC{
				url:        "127.0.0.1:49133",
				Received:   make(map[string]*xdsapi.DiscoveryResponse),
				Updates:    make(chan string),
				XDSUpdates: make(chan *xdsapi.DiscoveryResponse),
				RecvWg:     sync.WaitGroup{},
				cfg: &Config{
					Watch: make([]string, 0),
				},
			},
			port: uint32(49133),
			streamHandler: func(server xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
				return nil
			},
			expectedADSResources: &ADSC{
				Received: map[string]*xdsapi.DiscoveryResponse{},
			},
		},
		{
			desc: "stream-2-unnamed-resources",
			inAdsc: &ADSC{
				url:        "127.0.0.1:49133",
				Received:   make(map[string]*xdsapi.DiscoveryResponse),
				Updates:    make(chan string),
				XDSUpdates: make(chan *xdsapi.DiscoveryResponse),
				RecvWg:     sync.WaitGroup{},
				cfg: &Config{
					Watch: make([]string, 0),
				},
			},
			port: uint32(49133),
			streamHandler: func(stream xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
				_ = stream.Send(&xdsapi.DiscoveryResponse{
					TypeUrl: "foo",
				})
				_ = stream.Send(&xdsapi.DiscoveryResponse{
					TypeUrl: "bar",
				})
				return nil
			},
			expectedADSResources: &ADSC{
				Received: map[string]*xdsapi.DiscoveryResponse{
					"foo": {
						TypeUrl: "foo",
					},
					"bar": {
						TypeUrl: "bar",
					},
				},
			},
		},
		//todo tests for listeners, clusters, eds, and routes, not sure how to do this.
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			StreamHandler = tt.streamHandler
			l, err := net.Listen("tcp", ":"+fmt.Sprint(tt.port))
			if err != nil {
				t.Errorf("Unable to listen on port %v with tcp err %v", tt.port, err)
			}
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
			}
			tt.inAdsc.RecvWg.Add(1)
			err = tt.inAdsc.Run()
			tt.inAdsc.RecvWg.Wait()
			if !cmp.Equal(tt.inAdsc.Received, tt.expectedADSResources.Received, protocmp.Transform()) {
				t.Errorf("%s: expected recv %v got %v", tt.desc, tt.expectedADSResources.Received, tt.inAdsc.Received)
			}
		})
	}
}

func TestADSC_Save(t *testing.T) {
	tests := []struct {
		desc         string
		base         string
		expectedJSON map[string]string
		adsc         *ADSC
		err          error
	}{
		{
			desc: "empty",
			base: "out/test",
			expectedJSON: map[string]string{
				"_lds_tcp":  `{}`,
				"_lds_http": `{}`,
				"_rds":      `{}`,
				"_eds":      `{}`,
				"_ecds":     `{}`,
				"_cds":      `{}`,
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
			base: "out/test",
			err:  nil,
			expectedJSON: map[string]string{
				"_lds_tcp": `{
    "listener-1": {
      "name": "bar"
    },
    "listener-2": {
      "name": "mar"
    }
  }`,
				"_lds_http": `{
    "http-list-1": {
      "name": "bar"
    },
    "http-list-2": {
      "name": "mar"
    }
  }`,
				"_rds": `{
    "route-1": {
      "name": "mar"
    }
  }`,
				"_eds": `{
    "load-assignment-1": {
      "cluster_name": "foo"
    }
  }`,
				"_ecds": `{
    "eds-cluster-1": {
      "name": "test",
      "ClusterDiscoveryType": null,
      "LbConfig": null
    }
  }`,
				"_cds": `{
    "cluster-1": {
      "name": "foo",
      "ClusterDiscoveryType": null,
      "LbConfig": null
    }
  }`,
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
			_ = os.Mkdir("out", 0777)
			if err := tt.adsc.Save(tt.base); (err == nil && tt.err != nil) || (err != nil && tt.err == nil) {
				t.Errorf("AdscSave() => %v expected err %v", err, tt.err)
			}
			if ldsTCP := readFile(tt.base+"_lds_tcp.json", t); ldsTCP != tt.expectedJSON["_lds_tcp"] {
				t.Errorf("AdscSave() => %s expected ldsTcp %s", ldsTCP, tt.expectedJSON["_lds_tcp"])
			}
			if ldsHTTP := readFile(tt.base+"_lds_http.json", t); ldsHTTP != tt.expectedJSON["_lds_http"] {
				t.Errorf("AdscSave() => %s expected ldsHttp %s", ldsHTTP, tt.expectedJSON["_lds_http"])
			}
			if rds := readFile(tt.base+"_rds.json", t); rds != tt.expectedJSON["_rds"] {
				t.Errorf("AdscSave() => %s expected rds %s", rds, tt.expectedJSON["_rds"])
			}
			if ecds := readFile(tt.base+"_ecds.json", t); ecds != tt.expectedJSON["_ecds"] {
				t.Errorf("AdscSave() => %s expected ecds %s", ecds, tt.expectedJSON["_ecds"])
			}
			if cds := readFile(tt.base+"_cds.json", t); cds != tt.expectedJSON["_cds"] {
				t.Errorf("AdscSave() => %s expected cds %s", cds, tt.expectedJSON["_cds"])
			}
			if eds := readFile(tt.base+"_eds.json", t); eds != tt.expectedJSON["_eds"] {
				t.Errorf("AdscSave() => %s expected eds %s", eds, tt.expectedJSON["_eds"])
			}
			saveTeardown(tt.base, t)
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
	dat, err := ioutil.ReadFile(dir)
	if err != nil {
		t.Fatalf("file %s issue: %v", dat, err)
	}
	return string(dat)
}
