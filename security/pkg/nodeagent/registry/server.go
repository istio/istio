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

package registry

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"

	"github.com/gogo/googleapis/google/rpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/cmd/node_agent_k8s/workload/handler"
	"istio.io/istio/security/pkg/nodeagent/secrets"
	nvm "istio.io/istio/security/pkg/nodeagent/vm"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	pb "istio.io/istio/security/proto"
)

// Server specifies the node agent server.
// TODO(incfly): adds sync.Mutex to protects the fields, such as `handlerMap`.
type Server struct {
	// map from uid to workload handler instance.
	handlerMap map[string]handler.WorkloadHandler
	// makes mgmt-api server to stop
	done      chan bool
	config    *nvm.Config
	retriever Retriever
	// the workload identity running together with the NodeAgent, only used for vm mode.
	// TODO(incfly): uses this once Server supports vm mode.
	identity string // nolint
	// secretServer manages the secrets associated for different workload.
	secretServer secrets.SecretServer
}

// Retriever is the interface responsible for retrieving key/cert from upstream CA.
type Retriever interface {
	Retrieve(opt *pkiutil.CertOptions) (newCert, certChain, privateKey []byte, err error)
}

// New creates an NodeAgent server.
func New(cfg *nvm.Config, retriever Retriever) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("unablet to create when config is nil")
	}
	ss, err := secrets.NewSecretServer(&secrets.Config{
		Mode:            secrets.SecretFile,
		SecretDirectory: cfg.SecretDirectory,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init nodeagent due to secret server %v", err)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create caclient err %v", err)
	}
	return &Server{
		done:         make(chan bool, 1),
		handlerMap:   make(map[string]handler.WorkloadHandler),
		retriever:    retriever,
		config:       cfg,
		secretServer: ss,
	}, nil
}

// startLoop loops for the VM mode, i.e not running on k8s.
// TODO(inclfy): copy paste from nodeagent.go
func (s *Server) startLoop() { //nolint
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
		log.Errorf("failed to listen at unix domain socket %v", err)
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
	uid := request.Attrs.Uid
	sa := request.Attrs.Serviceaccount
	ns := request.Attrs.Namespace
	wlstr := fmt.Sprintf("ns = %v, service account = %v, uid = %v", ns, sa, uid)
	log.Infof("workload %v added", wlstr)
	if _, ok := s.handlerMap[uid]; ok {
		status := &rpc.Status{Code: int32(rpc.ALREADY_EXISTS), Message: "Already exists"}
		log.Infof("workload %v already exists", request)
		return &pb.NodeAgentMgmtResponse{Status: status}, nil
	}

	logReturn := func(tmpl string, err error) error {
		msg := fmt.Sprintf("%v for %v error %v", tmpl, wlstr, err)
		log.Errorf(msg)
		return fmt.Errorf(msg)
	}

	// Send CSR request to CA.
	host, err := pkiutil.GenSanURI(ns, sa)
	if err != nil {
		return nil, logReturn("failed to generate san uri", err)
	}
	cert, chain, priv, err := s.retriever.Retrieve(&pkiutil.CertOptions{
		Host:       host,
		Org:        s.config.CAClientConfig.Org,
		RSAKeySize: s.config.CAClientConfig.RSAKeySize,
		TTL:        s.config.CAClientConfig.RequestedCertTTL,
	})
	if err != nil {
		return nil, logReturn("csr request failed", err)
	}
	rootCert, err := ioutil.ReadFile(s.config.CAClientConfig.RootCertFile)
	if err != nil {
		return nil, logReturn("failed to load root cert err", err)
	}
	kb, err := pkiutil.NewVerifiedKeyCertBundleFromPem(cert, priv, chain, rootCert)
	if err != nil {
		return nil, logReturn("failed to build key cert bundle", err)
	}

	if err := s.secretServer.Put(sa, kb); err != nil {
		return nil, logReturn("failed to save key cert", err)
	}

	s.handlerMap[uid] = handler.NewHandler(request, s.config.WorkloadOpts)
	go s.handlerMap[uid].Serve()

	status := &rpc.Status{Code: int32(rpc.OK), Message: "OK"}
	return &pb.NodeAgentMgmtResponse{Status: status}, nil
}

// WorkloadDeleted defines the server side action when a workload is deleted.
func (s *Server) WorkloadDeleted(ctx context.Context, request *pb.WorkloadInfo) (*pb.NodeAgentMgmtResponse, error) {
	uid := request.Attrs.Uid
	if _, ok := s.handlerMap[uid]; !ok {
		status := &rpc.Status{Code: int32(rpc.NOT_FOUND), Message: "Not found"}
		return &pb.NodeAgentMgmtResponse{Status: status}, nil
	}

	log.Infof("Uid %s: Stop.", uid)
	s.handlerMap[uid].Stop()
	s.handlerMap[uid].WaitDone()
	delete(s.handlerMap, uid)

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
