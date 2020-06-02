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
	"log"
	"net"
	"reflect"
	"sync"
	"testing"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"
)

type testAdscRunServer struct{}

var StreamHandler func(stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error

func (t *testAdscRunServer) StreamAggregatedResources(stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	return StreamHandler(stream)
}

func (t *testAdscRunServer) DeltaAggregatedResources(ads.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return nil
}

func TestADSC_Run(t *testing.T) {
	tests := []struct {
		desc                 string
		inAdsc               *ADSC
		port                 uint32
		streamHandler        func(server ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error
		expectedADSResources *ADSC
	}{
		{
			desc: "stream-no-resources",
			inAdsc: &ADSC{
				certDir:    "",
				url:        "127.0.0.1:49133",
				Received:   make(map[string]*v2.DiscoveryResponse),
				Updates:    make(chan string),
				XDSUpdates: make(chan *v2.DiscoveryResponse),
				RecvWg:     sync.WaitGroup{},
				cfg: &Config{
					Watch: make([]string, 0),
				},
			},
			port: uint32(49133),
			streamHandler: func(server ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
				return nil
			},
			expectedADSResources: &ADSC{
				Received: map[string]*v2.DiscoveryResponse{},
			},
		},
		{
			desc: "stream-2-unnamed-resources",
			inAdsc: &ADSC{
				certDir:    "",
				url:        "127.0.0.1:49133",
				Received:   make(map[string]*v2.DiscoveryResponse),
				Updates:    make(chan string),
				XDSUpdates: make(chan *v2.DiscoveryResponse),
				RecvWg:     sync.WaitGroup{},
				cfg: &Config{
					Watch: make([]string, 0),
				},
			},
			port: uint32(49133),
			streamHandler: func(stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
				_ = stream.Send(&v2.DiscoveryResponse{
					TypeUrl: "foo",
				})
				_ = stream.Send(&v2.DiscoveryResponse{
					TypeUrl: "bar",
				})
				return nil
			},
			expectedADSResources: &ADSC{
				Received: map[string]*v2.DiscoveryResponse{
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
			ads.RegisterAggregatedDiscoveryServiceServer(xds, new(testAdscRunServer))
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
			if !reflect.DeepEqual(tt.inAdsc.Received, tt.expectedADSResources.Received) {
				t.Errorf("%s: expected recv %v got %v", tt.desc, tt.expectedADSResources.Received, tt.inAdsc.Received)
			}
		})
	}
}
