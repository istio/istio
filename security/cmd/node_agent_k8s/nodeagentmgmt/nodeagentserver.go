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

	rpc "github.com/gogo/googleapis/google/rpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/cmd/node_agent_k8s/workload/handler"
	pb "istio.io/istio/security/proto"
)

// ServerOptions contains the configuration for NodeAgent server.
type ServerOptions struct {
	// WorkloadOpts configures how we creates handler for each workload.
	WorkloadOpts handler.Options
}

// Server specifies the node agent server.
// TODO(incfly): adds sync.Mutex to protects the fields, such as `handlerMap`.
type Server struct {
	handlerMap map[string]handler.WorkloadHandler
	done       chan bool // makes mgmt-api server to stop
	opts       ServerOptions
}

// NewServer creates a NodeAgent server.
func NewServer(options ServerOptions) *Server {
	return &Server{
		done:       make(chan bool, 1),
		opts:       options,
		handlerMap: make(map[string]handler.WorkloadHandler),
	}
}

// Stop terminates the UDS.
func (s *Server) Stop() {
	s.done <- true
}

// WaitDone waits for a response back from Workloadhandler
func (s *Server) WaitDone() {
	<-s.done
}

// Serve opens the UDS channel and starts to serve NodeAgentService on that uds channel.
func (s *Server) Serve(path string) {
	grpcServer := grpc.NewServer()
	pb.RegisterNodeAgentServiceServer(grpcServer, s)

	var lis net.Listener
	var err error
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

	go func(ln net.Listener, s *Server) {
		<-s.done
		_ = ln.Close()
		s.CloseAllWlds()
	}(lis, s)

	_ = grpcServer.Serve(lis)
}

// WorkloadAdded defines the server side action when a workload is added.
func (s *Server) WorkloadAdded(ctx context.Context, request *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	log.Infof("The request is %v", request)
	if _, ok := s.handlerMap[request.Attrs.Uid]; ok {
		status := &rpc.Status{Code: int32(rpc.ALREADY_EXISTS), Message: "Already exists"}
		return &pb.NodeAgentMgmtResponse{Status: status}, nil
	}

	s.handlerMap[request.Attrs.Uid] = handler.NewHandler(request, s.opts.WorkloadOpts)
	go s.handlerMap[request.Attrs.Uid].Serve()

	status := &rpc.Status{Code: int32(rpc.OK), Message: "OK"}
	return &pb.NodeAgentMgmtResponse{Status: status}, nil
}

// WorkloadDeleted defines the server side action when a workload is deleted.
func (s *Server) WorkloadDeleted(ctx context.Context, request *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	if _, ok := s.handlerMap[request.Attrs.Uid]; !ok {
		status := &rpc.Status{Code: int32(rpc.NOT_FOUND), Message: "Not found"}
		return &pb.NodeAgentMgmtResponse{Status: status}, nil
	}

	log.Infof("Uid %s: Stop.", request.Attrs.Uid)
	s.handlerMap[request.Attrs.Uid].Stop()
	s.handlerMap[request.Attrs.Uid].WaitDone()

	delete(s.handlerMap, request.Attrs.Uid)

	status := &rpc.Status{Code: int32(rpc.OK), Message: "OK"}
	return &pb.NodeAgentMgmtResponse{Status: status}, nil
}

// CloseAllWlds closes the paths.
func (s *Server) CloseAllWlds() {
	for _, wld := range s.handlerMap {
		wld.Stop()
	}
	for _, wld := range s.handlerMap {
		wld.WaitDone()
	}
}
