// Copyright 2018 Istio Authors
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

package nodeagentmgmt

import (
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pb "istio.io/istio/security/proto"
)

// NodeAgentClient specify a UDS client connection
type NodeAgentClient struct {
	conn  *grpc.ClientConn
	dest  string
	isUds bool
}

// unixDialer connect a target with specified timeout.
func unixDialer(target string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", target, timeout)
}

// NewClient is used by the flexvolume driver to interface with the nodeagent grpc server
func NewClient(isUds bool, path string) *NodeAgentClient {
	c := new(NodeAgentClient)
	c.dest = path
	c.isUds = isUds
	return c
}

// ClientUds create a new client with path
func ClientUds(path string) *NodeAgentClient {
	return NewClient(true, path)
}

// client create a new client
func (c *NodeAgentClient) client() (pb.NodeAgentServiceClient, error) {
	var conn *grpc.ClientConn
	var err error
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithInsecure())
	if c.isUds == false {
		conn, err = grpc.Dial(c.dest, opts...)
		if err != nil {
			return nil, err
		}
	} else {
		opts = append(opts, grpc.WithDialer(unixDialer))
		conn, err = grpc.Dial(c.dest, opts...)
		if err != nil {
			return nil, err
		}
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func(conn *grpc.ClientConn, c chan os.Signal) {
		<-c
		conn.Close()
		os.Exit(0)
	}(conn, sigc)

	c.conn = conn
	return pb.NewNodeAgentServiceClient(conn), nil
}

// WorkloadAdded add the workload.
func (c *NodeAgentClient) WorkloadAdded(ninputs *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	cl, err := c.client()
	if err != nil {
		return nil, err
	}

	return cl.WorkloadAdded(context.Background(), ninputs)
}

// WorkloadDeleted delete the workload.
func (c *NodeAgentClient) WorkloadDeleted(ninputs *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	cl, err := c.client()
	if err != nil {
		return nil, err
	}

	return cl.WorkloadDeleted(context.Background(), ninputs)
}

// Close terminates the connection.
func (c *NodeAgentClient) Close() {
	if c.conn == nil {
		return
	}
	c.conn.Close()
}
