package mcpserver

import (
	"context"

	"google.golang.org/grpc"

	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/test/fakes/mcpserver"
)

type client struct {
	grpcClient mcpserver.McpSinkControllerServiceClient
}

func newClient(address string) (*client, error) {
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	c := mcpserver.NewMcpSinkControllerServiceClient(conn)
	return &client{
		grpcClient: c,
	}, nil
}

func (c *client) GetCollectionState(collection string) ([]*sink.Object, error) {
	req := &mcpserver.CollectionStateRequest{Collection: collection}
	resp, err := c.grpcClient.GetCollectionState(context.Background(), req)
	if err != nil {
		return nil, err
	}
	resources := resp.GetResources()
	return transformToSinkObject(resources), nil
}
