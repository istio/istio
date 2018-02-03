package mgmtwlhintf

import (
	"google.golang.org/grpc"

	pb "istio.io/istio/security/proto"
)

// Workloadhandler support this given interface.
// nodeagentmgmt will invoke
// Serve() as a go routine when a Workload is added.
// Stop() when a Workload is deleted.
// and WaitDone() to wait for a response back from Workloadhandler
type WorkloadMgmtInterface interface {
	Serve()
	Stop()
	WaitDone()
}

// NewWorkloadHandler is a function provided by workload handler that nodeagentmgmt will invoke
// to initialize the new workload handler when a new workload is added.
type NewWorkloadHandler func(info *pb.WorkloadInfo, wlS *WlServer, prefix string) WorkloadMgmtInterface

// RegisterGrpcServer is used by WorkloadAPI to register itself as the grpc server.
// It is invoked by the workload handler when it is initializing the workload socket.
type RegisterGrpcServer func(s *grpc.Server)

// WlServer is what the workload API implementor must fill out
type WlServer struct {
	SockFile	string
	RegAPI		RegisterGrpcServer
}

// WlHandler is used by NodeagentMgmt to create workload handler per workload.
type WlHandler struct {
	// Used to create a new Workload handler object.
	NewWlhCb	NewWorkloadHandler
	// Passed to workload handler to create the workload api grpc server.
	Wl	*WlServer
}

func NewWlHandler(wls *WlServer, cb NewWorkloadHandler) *WlHandler {
	return &WlHandler{
			Wl: wls,
			NewWlhCb: cb,
		}
}
