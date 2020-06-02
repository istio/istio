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
				url:        "127.0.0.1:49132",
				Received:   make(map[string]*v2.DiscoveryResponse),
				Updates:    make(chan string),
				XDSUpdates: make(chan *v2.DiscoveryResponse),
				RecvWg:     sync.WaitGroup{},
			},
			port: uint32(49132),
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
				url:        "127.0.0.1:49132",
				Received:   make(map[string]*v2.DiscoveryResponse),
				Updates:    make(chan string),
				XDSUpdates: make(chan *v2.DiscoveryResponse),
				RecvWg:     sync.WaitGroup{},
			},
			port: uint32(49132),
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
			log.Println("About to serve!")
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
