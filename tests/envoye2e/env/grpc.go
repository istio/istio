// Copyright 2020 Istio Authors
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

package env

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"istio.io/istio/tests/envoye2e/env/grpc_echo"
)

// GRPCServer provides a simple grpc echo service for testing.
type GRPCServer struct {
	port     uint16
	listener net.Listener

	grpc_echo.UnimplementedEchoServer
}

// NewGRPCServer configures a new GRPCServer. It does not attempt to
// start listening or anything else, however.
func NewGRPCServer(port uint16) *GRPCServer {
	return &GRPCServer{port: port}
}

// Start causes the GRPCServer to start a listener and begin serving.
func (g *GRPCServer) Start() <-chan error {
	errCh := make(chan error)
	addr := fmt.Sprintf("127.0.0.3:%d", g.port)
	go func() {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			errCh <- err
			return
		}
		g.listener = l
		s := grpc.NewServer()
		grpc_echo.RegisterEchoServer(s, g)
		reflection.Register(s)

		errCh <- s.Serve(l)
	}()

	go func() {
		errCh <- tryWaitForGRPCServer(addr)
	}()
	return errCh
}

// Stop closes the listener of the GRPCServer
func (g *GRPCServer) Stop() {
	g.listener.Close()
}

// Echo implements the grpc_echo service.
func (g *GRPCServer) Echo(ctx context.Context, req *grpc_echo.EchoRequest) (*empty.Empty, error) {
	return &empty.Empty{}, status.FromProto(req.ReturnStatus).Err()
}

func (g *GRPCServer) EchoStream(stream grpc_echo.Echo_EchoStreamServer) error {
	var i uint32
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		for i = 0; i < req.ResponseCount; i++ {
			if err = stream.Send(&empty.Empty{}); err != nil {
				return err
			}
		}
	}
}

func tryWaitForGRPCServer(addr string) error {
	for i := 0; i < 10; i++ {
		log.Println("Attempting to establish connection to gRPC server: ", addr)
		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		if err == nil {
			conn.Close()
			return nil
		}
		log.Println("Will wait 200ms and try again.")
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for gRPC server startup")
}
