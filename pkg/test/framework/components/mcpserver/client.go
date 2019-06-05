package mcpserver

import (
	"context"

	"google.golang.org/grpc"
	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/test/fakes/mcpserver"
	"istio.io/istio/pkg/test/scopes"
)

type client struct {
	grpcClient mcpserver.McpSinkControllerServiceClient
}

func newClient(address string) (*client, error) {
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		scopes.Framework.Errorf("unable to dial to %s. Error=%v", address, err)
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
		scopes.Framework.Errorf("error while obtaining response: %#v", err)
		return nil, err
	}
	resources := resp.GetResources()
	sinkObj, err := transformToSinkObject(resources)
	if err != nil {
		scopes.Framework.Errorf("error while transforming to sink.Object: %#v", err)
		return nil, err
	}
	return sinkObj, nil
}
