// Package sdsc includes a lightweight testing client to interact with SDS.
package sdsc

import (
	"context"
	"fmt"
	"time"

	"istio.io/pkg/log"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	authapi "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"

	sdscache "istio.io/istio/security/pkg/nodeagent/cache"
	agent_sds "istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/pki/util"
)

// Client is a lightweight client for testing secret discovery service server.
type Client struct {
	stream     sds.SecretDiscoveryService_StreamSecretsClient
	conn       *grpc.ClientConn
	updateChan chan xdsapi.DiscoveryResponse
	udsPath    string
}

// ClientOptions contains the options for the SDS testing.
type ClientOptions struct {
	ServerAddress string
}

// NewClient returns a sds client for testing.
func NewClient(options ClientOptions) (*Client, error) {
	address := fmt.Sprintf("unix://%s", options.ServerAddress)
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	client := sds.NewSecretDiscoveryServiceClient(conn)
	stream, err := client.StreamSecrets(context.Background())
	if err != nil {
		return nil, err
	}
	return &Client{
		stream:     stream,
		conn:       conn,
		updateChan: make(chan xdsapi.DiscoveryResponse, 1),
		udsPath:    address,
	}, nil
}

// Start starts sds client to receive the scecret updates from the server.
func (c *Client) Start() {
	go func() {
		msq, err := c.stream.Recv()
		if err != nil {
			log.Errorf("Connection closed %v", err)
			return
		}
		c.updateChan <- *msq
		log.Infof("receive response from sds server %v", msq)
	}()
}

// Stop stops the sds client.
func (c *Client) Stop() error {
	return c.stream.CloseSend()
}

// WaitForUpdate blocks until the error occurs or updates are pushed from the sds server.
func (c *Client) WaitForUpdate(duration time.Duration) (*xdsapi.DiscoveryResponse, error) {
	t := time.NewTimer(duration)
	for {
		select {
		case resp := <-c.updateChan:
			return &resp, nil
		case <-t.C:
			return nil, fmt.Errorf("timeout for updates")
		}
	}
}

// Send sends a request to the agent.
func (c *Client) Send() error {
	// TODO(incfly): just a place holder, need to follow xDS protocol.
	// - Initial request version is empty.
	// - Version & Nonce is needed for ack/rejecting.
	return c.stream.Send(&xdsapi.DiscoveryRequest{
		VersionInfo: "",
		ResourceNames: []string{
			sdscache.RootCertReqResourceName,
		},
		TypeUrl: agent_sds.SecretType,
	})
}

// ValidateResponse validates the SDS response.
func ValidateResponse(response *xdsapi.DiscoveryResponse, verify *util.VerifyFields) error {
	if response == nil {
		return fmt.Errorf("DiscoveryResponse is empty")
	}
	if len(response.Resources) != 1 {
		return fmt.Errorf("unexpected resource size in the response, %v ", response.Resources)
	}
	var pb authapi.Secret
	if err := types.UnmarshalAny(&response.Resources[0], &pb); err != nil {
		return fmt.Errorf("unmarshalAny SDS response failed: %v", err)
	}
	return nil
}
