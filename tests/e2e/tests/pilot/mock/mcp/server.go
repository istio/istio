package mcp

import (
	"fmt"
	"log"
	"net"
	"net/url"

	"github.com/gogo/protobuf/proto"
	google_protobuf "github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	mcpserver "istio.io/istio/galley/pkg/mcp/server"
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

type Server struct {
	// The internal snapshot.Cache that the server is using.
	Watcher *mockWatcher

	// TypeURLs that were originally passed in.
	TypeURLs []string

	// Port that the service is listening on.
	Port int

	// The gRPC compatible address of the service.
	URL *url.URL

	gs *grpc.Server
	l  net.Listener
}

func NewServer(port string, typeUrls []string) (*Server, error) {
	watcher := mockWatcher{}
	s := mcpserver.New(watcher, typeUrls)

	addr := fmt.Sprintf("127.0.0.1%s", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	p := l.Addr().(*net.TCPAddr).Port

	u, err := url.Parse(fmt.Sprintf("tcp://localhost:%d", p))
	if err != nil {
		_ = l.Close()
		return nil, err
	}

	gs := grpc.NewServer()

	mcp.RegisterAggregatedMeshConfigServiceServer(gs, s)
	go func() { _ = gs.Serve(l) }()

	return &Server{
		Watcher:  &watcher,
		TypeURLs: typeUrls,
		Port:     p,
		URL:      u,
		gs:       gs,
		l:        l,
	}, nil
}

func (t *Server) Close() (err error) {
	if t.gs != nil {
		t.gs.GracefulStop()
		t.gs = nil
	}

	t.l = nil // gRPC stack will close this
	t.Watcher = nil
	t.TypeURLs = nil
	t.Port = 0

	return
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
				"*.example.com",
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
				"*.example.org",
			},
		},
	},
}
