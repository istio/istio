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

package node_agent_k8s

import (
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
  "istio.io/istio/pkg/log"
  rpc "istio.io/gogo-genproto/googleapis/google/rpc"
  pb "istio.io/istio/security/proto"
)

// Server specify the node agent server.
type Server struct {
	wlmgmts     map[string]WorkloadMgmtInterface
	pathPrefix string
	done       chan bool //main 2 mgmt-api server to stop
	wli		*WlHandler
}

// Client specify a UDS client connection
type Client struct {
	conn  *grpc.ClientConn
	dest  string
	isUds bool
}

// NewServer create a new server.
func NewServer(pathPrefix string, wli *WlHandler) *Server {
	return &Server{
		done: make(chan bool, 1),
		pathPrefix: pathPrefix,
		wli: wli,
		wlmgmts: make(map[string]WorkloadMgmtInterface),
	}
}

// Stop terminate the UDS.
func (s *Server) Stop() {
	s.done <- true
}

// WaitDone to wait for a response back from Workloadhandler
func (s *Server) WaitDone() {
	<-s.done
}

// Serve opens the UDS channel
func (s *Server) Serve(isUds bool, path string) {
	grpcServer := grpc.NewServer()
	pb.RegisterNodeAgentServiceServer(grpcServer, s)

	var lis net.Listener
	var err error
	if isUds == false {
		lis, err = net.Listen("tcp", path)
		if err != nil {
			log.Errorf("failed to %v", err)
		}
	} else {
		_, e := os.Stat(path)
		if e == nil {
			e := os.RemoveAll(path)
			if e != nil {
				log.Errorf("failed to %s with error %v", path, err)
			}
		}
		lis, err = net.Listen("unix", path)
		if err != nil {
			log.Errorf("failed to %v", err)
		}
	}

	go func(ln net.Listener, s *Server) {
		<-s.done
		ln.Close()
		s.CloseAllWlds()
	}(lis, s)

	grpcServer.Serve(lis)
}

// WorkloadAdded define the server side action when a workload is added.
func (s *Server) WorkloadAdded(ctx context.Context, request *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	log.Infof("The request is %v", request)
	if _, ok := s.wlmgmts[request.Attrs.Uid]; ok == true {
		status := &rpc.Status{Code: int32(rpc.ALREADY_EXISTS), Message: "Already exists"}
		return &pb.NodeAgentMgmtResponse{Status: status}, nil
	}

	s.wlmgmts[request.Attrs.Uid] = s.wli.NewWlhCb(request, s.wli.Wl, s.pathPrefix)
	go s.wlmgmts[request.Attrs.Uid].Serve()

  status := &rpc.Status{Code: int32(rpc.OK), Message: "OK"}
	return &pb.NodeAgentMgmtResponse{Status: status}, nil
}

// WorkloadDeleted define the server side action when a workload is deleted.
func (s *Server) WorkloadDeleted(ctx context.Context, request *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	if _, ok := s.wlmgmts[request.Attrs.Uid]; ok == false {
    status := &rpc.Status{Code: int32(rpc.NOT_FOUND), Message: "Not found"}
		return &pb.NodeAgentMgmtResponse{Status: status}, nil
	}

  log.Infof("Uid %s: Stop.", request.Attrs.Uid)
	s.wlmgmts[request.Attrs.Uid].Stop()
	s.wlmgmts[request.Attrs.Uid].WaitDone()

	delete(s.wlmgmts, request.Attrs.Uid)

	status := &rpc.Status{Code: int32(rpc.OK), Message: "OK"}
	return &pb.NodeAgentMgmtResponse{Status: status}, nil
}

func (s *Server) Check(ctx context.Context, request *pb.CheckRequest) (*pb.CheckResponse, error) {
  // TODO(wattli): consolidate this.
  return nil, nil
}

func (s *Server) CloseAllWlds() {
	for _, wld := range s.wlmgmts {
		wld.Stop()
	}
	for _, wld := range s.wlmgmts {
		wld.WaitDone()
	}
}

// unixDialer connect a target with specified timeout.
func unixDialer(target string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", target, timeout)
}

// NewClient is used by the flexvolume driver to interface with the nodeagent grpc server
func NewClient(isUds bool, path string) *Client {
	c := new(Client)
	c.dest = path
	c.isUds = isUds
	return c
}

// ClientUds create a new client with path
func ClientUds(path string) *Client {
	return NewClient(true, path)
}

// client create a new client
func (c *Client) client() (pb.NodeAgentServiceClient, error) {
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
func (c *Client) WorkloadAdded(ninputs *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	cl, err := c.client()
	if err != nil {
		return nil, err
	}

	return cl.WorkloadAdded(context.Background(), ninputs)
}

// WorkloadDeleted delete the workload.
func (c *Client) WorkloadDeleted(ninputs *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	cl, err := c.client()
	if err != nil {
		return nil, err
	}

	return cl.WorkloadDeleted(context.Background(), ninputs)
}

// Close terminates the connection.
func (c *Client) Close() {
	if c.conn == nil {
		return
	}
	c.conn.Close()
}
