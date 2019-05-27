package mcpserver

import (
	fmt "fmt"
	"net"
	"time"

	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"

	"istio.io/istio/pkg/test/scopes"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/mcp/sink"
)

const (
	FakeServerPort = 6666
)

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
	scopes.Framework.Infof("collection=%s, sinkObjects=%v", req.GetCollection(), sinkObjects)
	protoResp := &CollectionStateResponse{
		Resources: convertToProtoSinkObjects(sinkObjects),
	}
	return protoResp, nil
}

func (srv *McpSinkServer) Start(port int) error {
	sinkListener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Printf("mcpserver.listen: %v", err)
		return err
	}
	fmt.Sprintf("server listening at %s", sinkListener.Addr().String())

	s := grpc.NewServer()
	gsrv := sink.NewServer(&srv.sinkOptions, &sink.ServerOptions{
		AuthChecker: &server.AllowAllChecker{},
		RateLimiter: rate.NewRateLimiter(time.Millisecond, 1000).Create(),
	})
	mcp.RegisterResourceSinkServer(s, gsrv)
	RegisterMcpSinkControllerServiceServer(s, srv)
	go func() {
		if err := srv.grpcSrv.Serve(sinkListener); err != nil {
			fmt.Printf("mcpserver.serve: %v", err)
		}
	}()
	srv.grpcSrv = s
	return nil
}

func (srv *McpSinkServer) Stop() {
	srv.grpcSrv.GracefulStop()
}
