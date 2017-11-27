// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package perf

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/mock"
)

// client encapsulates a Mixer client, for the purposes of perf testing.
type client struct {
	mixer mixerpb.MixerClient
	conn  *grpc.ClientConn
	setup *Setup
}

// initialize is the first method to be called. The client is expected to perform initialization by setting up
// any local state using setup, and connecting to the Mixer rpc server at the given address.
func (c *client) initialize(address string, setup *Setup) error {
	mixer, conn, err := mock.NewClient(address)
	if err != nil {
		return err
	}

	c.mixer = mixer
	c.conn = conn
	c.setup = setup

	return nil
}

// shutdown indicates that the client should perform graceful shutdown and cleanup of resources and get ready
// to exit.
func (c *client) shutdown() {
	if c.conn != nil {
		_ = c.conn.Close()

		c.conn = nil
		c.setup = nil
		c.mixer = nil
	}
}

// run indicates that the client should execute the load against the Mixer rpc server multiple times,
// as indicated by the iterations parameter.
func (c *client) run(iterations int) error {
	requests := c.setup.Load.createRequestProtos(c.setup.Config)

	for i := 0; i < iterations; i++ {

		for _, r := range requests {
			switch r.(type) {
			case *mixerpb.ReportRequest:
				c.mixer.Report(context.Background(), r.(*mixerpb.ReportRequest))

			case *mixerpb.CheckRequest:
				c.mixer.Check(context.Background(), r.(*mixerpb.CheckRequest))

			default:
				return fmt.Errorf("unknown request type: %v", r)
			}
		}
	}

	return nil
}
