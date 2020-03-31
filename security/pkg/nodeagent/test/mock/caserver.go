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

package mock

import (
	"context"
	"encoding/pem"
	"fmt"
	"net"
	"time"

	ghc "github.com/go-training/grpc-health-check/proto"
	"google.golang.org/grpc"

	"istio.io/istio/pkg/mcp/status"
	"istio.io/istio/pkg/spiffe"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	pb "istio.io/istio/security/proto"
	"istio.io/pkg/log"
)

var caServerLog = log.RegisterScope("ca", "CA service debugging", 0)

// CAServer is a mock CA server.
type CAServer struct {
	URL        string
	GRPCServer *grpc.Server

	certPem       []byte
	keyPem        []byte
	keyCertBundle util.KeyCertBundle
	certLifetime  time.Duration
}

// NewCAServer creates a new CA server that listens on port.
func NewCAServer(port int) (*CAServer, error) {
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
	keyCertBundle, err := util.NewVerifiedKeyCertBundleFromPem(cert, key, nil, cert)
	if err != nil {
		caServerLog.Errorf("failed to create CA KeyCertBundle: %+v", err)
		return nil, err
	}

	server := &CAServer{
		certPem:       cert,
		keyPem:        key,
		certLifetime:  24 * time.Hour,
		keyCertBundle: keyCertBundle,
		GRPCServer:    grpc.NewServer(),
	}
	// Register CA service at gRPC server.
	pb.RegisterIstioCertificateServiceServer(server.GRPCServer, server)
	ghc.RegisterHealthServer(server.GRPCServer, server)
	return server, server.start(port)
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
		log.Infof("start CA server on %s", s.URL)
		if err := s.GRPCServer.Serve(listener); err != nil {
			log.Errorf("CA Server failed to serve in %q: %v", s.URL, err)
		}
	}()
	return nil
}

// CreateCertificate handles CSR.
func (s *CAServer) CreateCertificate(ctx context.Context, request *pb.IstioCertificateRequest) (
	*pb.IstioCertificateResponse, error) {
	cert, err := s.sign([]byte(request.Csr), []string{"client-identity"}, time.Duration(request.ValidityDuration)*time.Second, false)
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
	signingCert, signingKey, _, _ := s.keyCertBundle.GetAll()
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

// Check implements `service Health`.
func (s *CAServer) Check(ctx context.Context, in *ghc.HealthCheckRequest) (*ghc.HealthCheckResponse, error) {
	return &ghc.HealthCheckResponse{
		Status: ghc.HealthCheckResponse_SERVING,
	}, nil
}
