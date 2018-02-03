package workloadapi

import (
	"fmt"
	"log"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

  rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	pb "istio.io/istio/security/proto"
  wlh "istio.io/istio/security/cmd/node_agent_k8s/workloadhandler"
  mwi "istio.io/istio/security/cmd/node_agent_k8s/mgmtwlhintf"
)

const (
	socName string = "/server.sock"
)

type WlServer struct {}

func NewWlAPIServer() *mwi.WlServer {
	return &mwi.WlServer{
		SockFile: socName,
		RegAPI: RegisterGrpc,
	}
}

func RegisterGrpc(s *grpc.Server) {
	pb.RegisterWorkloadServiceServer(s, &WlServer{})
}

func (s *WlServer) Check(ctx context.Context, request *pb.CheckRequest) (*pb.CheckResponse, error) {

	log.Printf("[%v]: %v Check called", s, request)
	// Get the caller's credentials from the context.
	creds, e := wlh.CallerFromContext(ctx)
	if !e {
		resp := fmt.Sprint("Not able to get credentials")
    status := &rpc.Status{Code: int32(rpc.PERMISSION_DENIED), Message: resp}
		return &pb.CheckResponse{Status: status}, nil
	}

	log.Printf("Credentials are %v", creds)

	resp := fmt.Sprintf("all good to workload with service account %v", creds.ServiceAccount)
  status := &rpc.Status{Code: int32(rpc.OK), Message: resp}
	return &pb.CheckResponse{Status: status}, nil
}
