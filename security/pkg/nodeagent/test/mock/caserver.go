// Copyright Istio Authors
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

package mock

import (
	"context"
	"encoding/pem"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	ghc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/server/ca"
	"istio.io/pkg/log"
)

var caServerLog = log.RegisterScope("ca", "CA service debugging", 0)

// CAServer is a mock CA server.
type CAServer struct {
	pb.UnimplementedIstioCertificateServiceServer
	URL            string
	GRPCServer     *grpc.Server
	Authenticators []security.Authenticator

	certPem       []byte
	keyPem        []byte
	KeyCertBundle *util.KeyCertBundle
	certLifetime  time.Duration

	rejectCSR       bool
	emptyCert       bool
	faultInjectLock *sync.Mutex
}

func NewCAServerWithKeyCert(port int, key, cert []byte, opts ...grpc.ServerOption) (*CAServer, error) {
	keyCertBundle, err := util.NewVerifiedKeyCertBundleFromPem(cert, key, nil, cert)
	if err != nil {
		caServerLog.Errorf("failed to create CA KeyCertBundle: %+v", err)
		return nil, err
	}

	server := &CAServer{
		certPem:         cert,
		keyPem:          key,
		certLifetime:    24 * time.Hour,
		KeyCertBundle:   keyCertBundle,
		GRPCServer:      grpc.NewServer(opts...),
		faultInjectLock: &sync.Mutex{},
	}
	// Register CA service at gRPC server.
	pb.RegisterIstioCertificateServiceServer(server.GRPCServer, server)
	ghc.RegisterHealthServer(server.GRPCServer, server)
	return server, server.start(port)
}

// NewCAServer creates a new CA server that listens on port.
func NewCAServer(port int, opts ...grpc.ServerOption) (*CAServer, error) {
	// Create root cert and private key.
	options := util.CertOptions{
		TTL:          3650 * 24 * time.Hour,
		Org:          spiffe.GetTrustDomain(),
		IsCA:         true,
		IsSelfSigned: true,
		RSAKeySize:   2048,
		IsDualUse:    true,
	}
	cert, key, err := util.GenCertKeyFromOptions(options)
	if err != nil {
		caServerLog.Errorf("cannot create CA cert and private key: %+v", err)
		return nil, err
	}
	return NewCAServerWithKeyCert(port, key, cert, opts...)
}

func (s *CAServer) start(port int) error {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		caServerLog.Errorf("cannot listen on port %d (error: %v)", port, err)
		return err
	}

	// If passed in port is 0, get the actual chosen port.
	port = listener.Addr().(*net.TCPAddr).Port
	s.URL = fmt.Sprintf("localhost:%d", port)
	go func() {
		caServerLog.Infof("start CA server on %s", s.URL)
		if err := s.GRPCServer.Serve(listener); err != nil && (err != grpc.ErrServerStopped) {
			caServerLog.Errorf("CA Server failed to serve in %q: %v", s.URL, err)
		}
	}()
	return nil
}

// RejectCSR specifies whether to send error response to CSR.
func (s *CAServer) RejectCSR(reject bool) {
	s.faultInjectLock.Lock()
	s.rejectCSR = reject
	s.faultInjectLock.Unlock()
	if reject {
		caServerLog.Info("force CA server to return error to CSR")
	}
}

func (s *CAServer) shouldReject() bool {
	var reject bool
	s.faultInjectLock.Lock()
	reject = s.rejectCSR
	s.faultInjectLock.Unlock()
	return reject
}

// SendEmptyCert force CA server send empty cert chain.
func (s *CAServer) SendEmptyCert() {
	s.faultInjectLock.Lock()
	s.emptyCert = true
	s.faultInjectLock.Unlock()
	caServerLog.Info("force CA server to send empty cert chain")
}

func (s *CAServer) sendEmpty() bool {
	var empty bool
	s.faultInjectLock.Lock()
	empty = s.emptyCert
	s.faultInjectLock.Unlock()
	return empty
}

// CreateCertificate handles CSR.
func (s *CAServer) CreateCertificate(ctx context.Context, request *pb.IstioCertificateRequest) (
	*pb.IstioCertificateResponse, error,
) {
	caServerLog.Infof("received CSR request")
	if s.shouldReject() {
		caServerLog.Info("force rejecting CSR request")
		return nil, status.Error(codes.Unavailable, "CA server is not available")
	}
	if s.sendEmpty() {
		caServerLog.Info("force sending empty cert chain in CSR response")
		response := &pb.IstioCertificateResponse{
			CertChain: []string{},
		}
		return response, nil
	}
	id := []string{"client-identity"}
	if len(s.Authenticators) > 0 {
		caller := ca.Authenticate(ctx, s.Authenticators)
		if caller == nil {
			return nil, status.Error(codes.Unauthenticated, "request authenticate failure")
		}
		id = caller.Identities
	}
	cert, err := s.sign([]byte(request.Csr), id, time.Duration(request.ValidityDuration)*time.Second, false)
	if err != nil {
		caServerLog.Errorf("failed to sign CSR: %+v", err)
		return nil, status.Errorf(err.(*caerror.Error).HTTPErrorCode(), "CSR signing error: %+v", err.(*caerror.Error))
	}
	respCertChain := []string{string(cert)}
	respCertChain = append(respCertChain, string(s.certPem))
	response := &pb.IstioCertificateResponse{
		CertChain: respCertChain,
	}
	caServerLog.Info("send back CSR success response")
	return response, nil
}

func (s *CAServer) sign(csrPEM []byte, subjectIDs []string, _ time.Duration, forCA bool) ([]byte, error) {
	csr, err := util.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		caServerLog.Errorf("failed to parse CSR: %+v", err)
		return nil, caerror.NewError(caerror.CSRError, err)
	}
	signingCert, signingKey, _, _ := s.KeyCertBundle.GetAll()
	certBytes, err := util.GenCertFromCSR(csr, signingCert, csr.PublicKey, *signingKey, subjectIDs, s.certLifetime, forCA)
	if err != nil {
		caServerLog.Errorf("failed to generate cert from CSR: %+v", err)
		return nil, caerror.NewError(caerror.CertGenError, err)
	}
	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}
	cert := pem.EncodeToMemory(block)

	return cert, nil
}

// Check handles health check requests.
func (s *CAServer) Check(ctx context.Context, in *ghc.HealthCheckRequest) (*ghc.HealthCheckResponse, error) {
	return &ghc.HealthCheckResponse{
		Status: ghc.HealthCheckResponse_SERVING,
	}, nil
}

// Watch handles health check streams.
func (s *CAServer) Watch(_ *ghc.HealthCheckRequest, _ ghc.Health_WatchServer) error {
	return nil
}
