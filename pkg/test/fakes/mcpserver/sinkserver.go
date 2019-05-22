package mcpserver

import (
	"flag"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gogo/protobuf/types"
	context "golang.org/x/net/context"
	mcp "istio.io/api/mcp/v1alpha1"
	v1alpha1 "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/test/scopes"

	"google.golang.org/grpc"
	"istio.io/istio/pkg/mcp/testing/monitoring"

	"istio.io/istio/pkg/mcp/sink"
)

const (
	FakeServerPort = 6666
)

type mcpSinkServer struct {
	upd *sink.InMemoryUpdater
}

func main() {
	fakeMcpSinkPort := flag.Int("port", FakeServerPort, "port on which fake MCP server (in sink mode) listens")
	sinkConfigCollection := flag.String("sink-config", "", "sink mode configuration for MCP server")
	flag.Parse()

	sinkConfigColl := strings.Split(*sinkConfigCollection, ",")
	inMemoryUpdater := sink.NewInMemoryUpdater()
	so := sink.Options{
		ID:                "mcpserver.sink",
		CollectionOptions: sink.CollectionOptionsFromSlice(sinkConfigColl),
		Updater:           inMemoryUpdater,
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}
	ctrlSrv := &mcpSinkServer{upd: inMemoryUpdater}

	sinkListener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", *fakeMcpSinkPort))
	if err != nil {
		scopes.Framework.Errorf("mcpserver.listen: %v", err)
		return
	}

	s := grpc.NewServer()
	srv := sink.NewServer(&so, &sink.ServerOptions{
		AuthChecker: &server.AllowAllChecker{},
		RateLimiter: rate.NewRateLimiter(time.Millisecond, 1000).Create(),
	})
	mcp.RegisterResourceSinkServer(s, srv)
	RegisterMcpSinkControllerServiceServer(s, ctrlSrv)

	if err := s.Serve(sinkListener); err != nil {
		scopes.Framework.Errorf("mcpserver.Serve: %v", err)
	}
}

func (srv *mcpSinkServer) GetCollectionState(ctx context.Context, req *CollectionStateRequest) (*CollectionStateResponse, error) {
	sinkObjects := srv.upd.Get(req.GetCollection())
	scopes.Framework.Infof("collection=%s, sinkObjects=%v", req.GetCollection(), sinkObjects)
	protoResp := &CollectionStateResponse{
		SinkObject: convertToProtoSinkObjects(sinkObjects),
	}
	return protoResp, nil
}

func convertToProtoSinkObjects(sos []*sink.Object) []*v1alpha1.Resource {
	protoSinkObjects := make([]*v1alpha1.Resource, 0)
	for _, so := range sos {
		pso := &v1alpha1.Resource{
			Metadata: so.Metadata,
			Body: &types.Any{
				TypeUrl: so.TypeURL,
				Value:   []byte(so.Body.String()),
			},
		}
		protoSinkObjects = append(protoSinkObjects, pso)
	}
	return protoSinkObjects
}
