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

package workloadhandler

import (
	"log"
	"net"
	"os"

	"google.golang.org/grpc"

	mwi "istio.io/istio/security/cmd/node_agent_k8s/mgmtwlhintf"
	pbmgmt "istio.io/istio/security/proto"
)

// Server is the WorkloadHandler (one per workload).
type Server struct {
	creds    *CredInfo
	filePath string
	done     chan bool
	wlS      *mwi.WlServer
}

// NewCreds return the new creds
func NewCreds(wli *pbmgmt.WorkloadInfo) *CredInfo {
	return &CredInfo{
		UID:            wli.Attrs.Uid,
		Name:           wli.Attrs.Workload,
		Namespace:      wli.Attrs.Namespace,
		ServiceAccount: wli.Attrs.Serviceaccount,
	}
}

// NewServer return the new server with default setup
func NewServer(wli *pbmgmt.WorkloadInfo, wlS *mwi.WlServer, pathPrefix string) mwi.WorkloadMgmtInterface {
	s := &Server{
		done:     make(chan bool, 1),
		creds:    NewCreds(wli),
		filePath: pathPrefix + "/" + wli.Attrs.Uid + wlS.SockFile,
		wlS:      wlS,
	}
	return s
}

// Serve adherence to nodeagent workload management interface.
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

// Stop tell the server it should stop
func (s *Server) Stop() {
	s.done <- true
}

// WaitDone for the server to stop and then return
func (s *Server) WaitDone() {
	<-s.done
}
