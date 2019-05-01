//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package pilot

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	adsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/hashicorp/go-multierror"

	"google.golang.org/grpc"
)

type client struct {
	discoveryAddr *net.TCPAddr
	conn          *grpc.ClientConn
	stream        adsapi.AggregatedDiscoveryService_StreamAggregatedResourcesClient
	lastRequest   *xdsapi.DiscoveryRequest

	wg sync.WaitGroup
}

func newClient(discoveryAddr *net.TCPAddr) (*client, error) {
	conn, err := grpc.Dial(discoveryAddr.String(), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	adsClient := adsapi.NewAggregatedDiscoveryServiceClient(conn)
	stream, err := adsClient.StreamAggregatedResources(context.Background())
	if err != nil {
		return nil, err
	}

	return &client{
		conn:          conn,
		stream:        stream,
		discoveryAddr: discoveryAddr,
	}, nil
}

func (c *client) CallDiscovery(req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	c.lastRequest = req
	err := c.stream.Send(req)
	if err != nil {
		return nil, err
	}
	return c.stream.Recv()
}

func (c *client) CallDiscoveryOrFail(t testing.TB, req *xdsapi.DiscoveryRequest) *xdsapi.DiscoveryResponse {
	resp, err := c.CallDiscovery(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (c *client) StartDiscovery(req *xdsapi.DiscoveryRequest) error {
	c.lastRequest = req
	err := c.stream.Send(req)
	if err != nil {
		return err
	}
	return nil
}

func (c *client) StartDiscoveryOrFail(t testing.TB, req *xdsapi.DiscoveryRequest) {
	if err := c.StartDiscovery(req); err != nil {
		t.Fatal(err)
	}
}

func (c *client) WatchDiscovery(timeout time.Duration,
	accept func(*xdsapi.DiscoveryResponse) (bool, error)) error {
	c1 := make(chan error, 1)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		for {
			result, err := c.stream.Recv()
			if err != nil {
				c1 <- err
				break
			}
			// ACK all responses so that when an update arrives we can receive it
			err = c.stream.Send(&xdsapi.DiscoveryRequest{
				Node:          c.lastRequest.Node,
				ResponseNonce: result.Nonce,
				VersionInfo:   result.VersionInfo,
				TypeUrl:       c.lastRequest.TypeUrl,
			})
			if err != nil {
				c1 <- err
				break
			}
			accepted, err := accept(result)
			if err != nil {
				c1 <- err
				break
			}
			if accepted {
				c1 <- nil
				break
			}
		}
	}()
	select {
	case err := <-c1:
		return err
	case <-time.After(timeout):
		return errors.New("timed out")
	}
}

func (c *client) WatchDiscoveryOrFail(t testing.TB, timeout time.Duration,
	accept func(*xdsapi.DiscoveryResponse) (bool, error)) {

	if err := c.WatchDiscovery(timeout, accept); err != nil {
		t.Fatalf("no resource accepted: %v", err)
	}
}

func (c *client) Close() (err error) {
	if c.stream != nil {
		err = multierror.Append(err, c.stream.CloseSend()).ErrorOrNil()
	}
	if c.conn != nil {
		err = multierror.Append(err, c.conn.Close()).ErrorOrNil()
	}

	c.wg.Wait()

	return
}
