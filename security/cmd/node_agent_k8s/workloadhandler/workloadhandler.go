package workloadhandler

import (
	"log"
	"net"
	"os"

	"google.golang.org/grpc"

  pbmgmt "istio.io/istio/security/proto"
  mwi "istio.io/istio/security/cmd/node_agent_k8s/mgmtwlhintf"
)

// The WorkloadHandler (one per workload).
type Server struct {
	creds		*CredInfo
	filePath       string
	done           chan bool
	wlS		*mwi.WlServer
}

func NewCreds(wli *pbmgmt.WorkloadInfo) *CredInfo {
	return &CredInfo{
		Uid: wli.Attrs.Uid,
		Name: wli.Attrs.Workload,
		Namespace: wli.Attrs.Namespace,
		ServiceAccount: wli.Attrs.Serviceaccount,
	}
}

func NewServer(wli *pbmgmt.WorkloadInfo, wlS *mwi.WlServer, pathPrefix string) mwi.WorkloadMgmtInterface {
	s := &Server{
		done: make(chan bool, 1),
		creds: NewCreds(wli),
		filePath: pathPrefix + "/" + wli.Attrs.Uid + wlS.SockFile,
		wlS: wlS,
	}
	return s
}

// WorkloadApi adherence to nodeagent workload management interface.
func (s *Server) Serve() {
	grpcServer := grpc.NewServer(grpc.Creds(s.GetCred()))
	s.wlS.RegAPI(grpcServer)

	var lis net.Listener
	var err error
	_, e := os.Stat(s.filePath)
	if e == nil {
		e := os.RemoveAll(s.filePath)
		if e != nil {
			log.Printf("Failed to rm %v (%v)", s.filePath, e)
			return
		}
	}

	lis, err = net.Listen("unix", s.filePath)
	if err != nil {
		log.Printf("failed to %v", err)
		return
	}

	go func(ln net.Listener, c chan bool) {
		<-c
		ln.Close()
		log.Printf("Closed the listener.")
		c <- true
	}(lis, s.done)

	log.Printf("workload [%v] listen", s)
	grpcServer.Serve(lis)
}

// Tell the server it should stop
func (s *Server) Stop() {
	s.done <- true
}

// Wait for the server to stop and then return
func (s *Server) WaitDone() {
	<-s.done
}
