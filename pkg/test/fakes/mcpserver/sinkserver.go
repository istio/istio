package mcpserver

import (
	fmt "fmt"
	"net"
	"time"

	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"

	"istio.io/pkg/log"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/mcp/sink"

	// include this to pull in appropriate protobuf types
	// so that protobuf won't complain that the type is
	// "not linked in"
	_ "istio.io/istio/galley/pkg/metadata"
)

const (
	FakeServerPort = 6666
)

var scope = log.RegisterScope("server", "MCP Sink server debugging", 0)

type McpSinkServer struct {
	upd         *sink.InMemoryUpdater
	sinkOptions sink.Options
	grpcSrv     *grpc.Server
}

func NewMcpSinkServer(sinkOptions sink.Options) *McpSinkServer {
	inMemoryUpdater := sink.NewInMemoryUpdater()
	sinkOptions.Updater = inMemoryUpdater
	return &McpSinkServer{
		upd:         inMemoryUpdater,
		sinkOptions: sinkOptions,
	}
}

func (srv *McpSinkServer) GetCollectionState(ctx context.Context, req *CollectionStateRequest) (*CollectionStateResponse, error) {
	sinkObjects := srv.upd.Get(req.GetCollection())
	protoResources, err := convertToProtoSinkObjects(sinkObjects)
	if err != nil {
		return nil, err
	}
	protoResp := &CollectionStateResponse{
		Resources: protoResources,
	}
	return protoResp, nil
}

func (srv *McpSinkServer) Start(port int) error {
	sinkListener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		scope.Fatalf("mcpserver.listen: %v", err)
		return err
	}
	scope.Infof("server listening at %s", sinkListener.Addr().String())

	s := grpc.NewServer()
	gsrv := sink.NewServer(&srv.sinkOptions, &sink.ServerOptions{
		AuthChecker: &server.AllowAllChecker{},
		RateLimiter: rate.NewRateLimiter(time.Millisecond, 1000).Create(),
	})
	mcp.RegisterResourceSinkServer(s, gsrv)
	RegisterMcpSinkControllerServiceServer(s, srv)
	go func() {
		if err := srv.grpcSrv.Serve(sinkListener); err != nil {
			scope.Fatalf("mcpserver.serve: %v", err)
		}
	}()
	srv.grpcSrv = s
	return nil
}

func (srv *McpSinkServer) Stop() {
	srv.grpcSrv.GracefulStop()
}
