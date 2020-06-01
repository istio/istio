package adsc

import (
	"fmt"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"
	"log"
	"net"
	"testing"
)

type testAdscRunServer struct {}

var StreamHandler func(stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error

func (t *testAdscRunServer) StreamAggregatedResources(stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	return StreamHandler(stream)
}

func (t *testAdscRunServer) DeltaAggregatedResources(ads.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return nil
}

func TestADSC_Run(t *testing.T) {
	type testAdscServer struct {}

	tests := []struct{
		desc string
		inAdsc *ADSC
		port uint32
		streamHandler func(server ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error
	}{
		{
			desc: "stream-no-resources",
			inAdsc: &ADSC{
				certDir: "",
				url: "127.0.0.1:49132",
			},
			port: uint32(49132),
			streamHandler: func(server ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			StreamHandler = tt.streamHandler
			l, err := net.Listen("tcp", ":" + fmt.Sprintf("%s", tt.port))
			if err != nil {
				t.Errorf("Unable to listen on port %v with tcp err %v", tt.port, err)
			}
			xds := grpc.NewServer()
			ads.RegisterAggregatedDiscoveryServiceServer(xds, new(testAdscRunServer))
			err = xds.Serve(l)
			if err != nil {
				t.Errorf("Could not start serving ads server %v", err)
			}
			log.Println(tt.inAdsc.Run())
		})
	}

}