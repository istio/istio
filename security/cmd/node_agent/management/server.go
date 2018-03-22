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

package management

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/gogo/googleapis/google/rpc"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/cmd/node_agent/na"
	"istio.io/istio/security/cmd/node_agent_k8s/workload/handler"
	cagrpc "istio.io/istio/security/pkg/caclient/grpc"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	"istio.io/istio/security/pkg/workload"
	pb "istio.io/istio/security/proto"
)

// Server specifies the node agent server.
// TODO(incfly): adds sync.Mutex to protects the fields, such as `handlerMap`.
type Server struct {
	// map from uid to workload handler instance.
	handlerMap map[string]handler.WorkloadHandler
	// makes mgmt-api server to stop
	done     chan bool
	config   *na.Config
	pc       platform.Client
	cAClient cagrpc.CAGrpcClient
	// the workload identity running together with the NodeAgent, only used for vm mode.
	identity string // nolint
	// ss manages the secrets associated for different workload.
	ss workload.SecretServer
}

// New creates an NodeAgent server.
func New(cfg *na.Config) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("unablet to create when config is nil")
	}
	pc, err := platform.NewClient(cfg.CAClientConfig.Env, cfg.CAClientConfig.RootCertFile, cfg.CAClientConfig.KeyFile,
		cfg.CAClientConfig.CertChainFile, cfg.CAClientConfig.CAAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to init nodeagent due to pc init %v", err)
	}
	scfg := workload.NewSecretFileServerConfig(cfg.CAClientConfig.CertChainFile, cfg.CAClientConfig.KeyFile)
	ss, err := workload.NewSecretServer(scfg)
	if err != nil {
		return nil, fmt.Errorf("failed to init nodeagent due to secret server %v", err)
	}
	return &Server{
		done:       make(chan bool, 1),
		handlerMap: make(map[string]handler.WorkloadHandler),
		cAClient:   &cagrpc.CAGrpcClientImpl{},
		config:     cfg,
		pc:         pc,
		ss:         ss,
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
	log.Infof("workload uid = %v, ns = %v, service account = %v added", uid, ns, sa)
	if _, ok := s.handlerMap[uid]; ok {
		status := &rpc.Status{Code: int32(rpc.ALREADY_EXISTS), Message: "Already exists"}
		log.Infof("workload %v already exists", request)
		return &pb.NodeAgentMgmtResponse{Status: status}, nil
	}

	// Sends CSR request to CA.
	// TODO(inclfy): extract the SPIFFE formatting out into somewhere else.
	id := fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/%s", ns, sa)

	// TODO(incfly): uses caClient's own createRequest when it supports multiple identity.
	priv, csrReq, err := s.createRequest(id)
	if err != nil {
		return nil, fmt.Errorf("failed to create csr request for service %v %v", sa, err)
	}
	resp, err := s.cAClient.SendCSR(csrReq, s.pc, s.config.CAClientConfig.CAAddress)
	if err != nil {
		return nil, fmt.Errorf("csr request failed for service %v %v", sa, err)
	}

	if err := s.ss.SetServiceIdentityPrivateKey(priv); err != nil {
		return nil, fmt.Errorf("failed in set private key for service account %v, uid %v err %v", sa, uid, err)
	}
	if err := s.ss.SetServiceIdentityCert(resp.SignedCert); err != nil {
		return nil, fmt.Errorf("failed in set cert for service account %v, uid %v err %v", sa, uid, err)
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

func (s *Server) createRequest(identity string) ([]byte, *pb.CsrRequest, error) {
	csr, privKey, err := pkiutil.GenCSR(pkiutil.CertOptions{
		Host:       identity,
		Org:        s.config.CAClientConfig.Org,
		RSAKeySize: s.config.CAClientConfig.RSAKeySize,
	})
	if err != nil {
		return nil, nil, err
	}

	cred, err := s.pc.GetAgentCredential()
	if err != nil {
		return nil, nil, fmt.Errorf("request creation fails on getting agent credential (%v)", err)
	}

	return privKey, &pb.CsrRequest{
		CsrPem:              csr,
		NodeAgentCredential: cred,
		CredentialType:      s.pc.GetCredentialType(),
		RequestedTtlMinutes: int32(s.config.CAClientConfig.RequestedCertTTL.Minutes()),
		ForCA:               false,
	}, nil
}
