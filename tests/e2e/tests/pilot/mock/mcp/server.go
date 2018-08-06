package main

import (
	"fmt"
	"log"
	"net"

	"github.com/gogo/protobuf/proto"
	google_protobuf "github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	mcpserver "istio.io/istio/galley/pkg/mcp/server"
	"istio.io/istio/pilot/pkg/model"
)

type mockWatcher struct{}

func (m mockWatcher) Watch(req *mcp.MeshConfigRequest,
	resp chan<- *mcpserver.WatchResponse) (*mcpserver.WatchResponse, mcpserver.CancelWatchFunc) {
	fmt.Printf("from mock server ============> %+v", req)
	marshaledFirstGateway, err := proto.Marshal(firstGateway)
	if err != nil {
		log.Fatalf("marshaling gateway %s\n", err)
	}
	marshaledSecondGateway, err := proto.Marshal(secondGateway)
	if err != nil {
		log.Fatalf("marshaling gateway %s\n", err)
	}

	var cancelFunc mcpserver.CancelWatchFunc
	cancelFunc = func() {
		fmt.Println("watch canceled")
	}

	return &mcpserver.WatchResponse{
		Version: req.GetVersionInfo(),
		TypeURL: req.GetTypeUrl(),
		Envelopes: []*mcp.Envelope{
			{
				Metadata: &mcp.Metadata{
					Name: "some-name",
				},
				Resource: &google_protobuf.Any{
					TypeUrl: req.GetTypeUrl(),
					Value:   marshaledFirstGateway,
				},
			},
			{
				Metadata: &mcp.Metadata{
					Name: "some-other-name",
				},
				Resource: &google_protobuf.Any{
					TypeUrl: req.GetTypeUrl(),
					Value:   marshaledSecondGateway,
				},
			},
		},
	}, cancelFunc
}

func main() {
	log.Println("starting server")
	listener, err := net.Listen("tcp", "0.0.0.0:15014")
	if err != nil {
		log.Fatalf("failed to setup listener: %v", err)
	}
	// load up all the supported typs for now??!
	typeURLBase := "type.googleapis.com/"
	supportedTypes := make([]string, len(model.IstioConfigTypes))
	for i, model := range model.IstioConfigTypes {
		supportedTypes[i] = typeURLBase + model.MessageName
	}

	grpcServer := grpc.NewServer([]grpc.ServerOption{}...)
	server := mcpserver.New(mockWatcher{}, supportedTypes)
	mcp.RegisterAggregatedMeshConfigServiceServer(grpcServer, server)

	log.Println("serving...")
	if err = grpcServer.Serve(listener); err != nil {
		log.Fatalf("failed to server: %v", err)
	}
}

var firstGateway = &networking.Gateway{
	Servers: []*networking.Server{
		&networking.Server{
			Port: &networking.Port{
				Name:     "http",
				Number:   8099,
				Protocol: "HTTP",
			},
			Hosts: []string{
				"*",
			},
		},
	},
}

var secondGateway = &networking.Gateway{
	Servers: []*networking.Server{
		&networking.Server{
			Port: &networking.Port{
				Name:     "tcp",
				Number:   880,
				Protocol: "TCP",
			},
			Hosts: []string{
				"*",
			},
		},
	},
}
