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

package ca

import (
	"fmt"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/pki/ca"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

var serverCaLog = log.RegisterScope("serverca", "Citadel server log", 0)

// CertificateAuthority contains methods to be supported by a CA.
type CertificateAuthority interface {
	// Sign generates a certificate for a workload or CA, from the given CSR and cert opts.
	Sign(csrPEM []byte, opts ca.CertOpts) ([]byte, error)
	// SignWithCertChain is similar to Sign but returns the leaf cert and the entire cert chain.
	SignWithCertChain(csrPEM []byte, opts ca.CertOpts) ([]string, error)
	// GetCAKeyCertBundle returns the KeyCertBundle used by CA.
	GetCAKeyCertBundle() *util.KeyCertBundle
}

// Server implements IstioCAService and IstioCertificateService and provides the services on the
// specified port.
type Server struct {
	pb.UnimplementedIstioCertificateServiceServer
	monitoring     monitoringMetrics
	Authenticators []security.Authenticator
	ca             CertificateAuthority
	serverCertTTL  time.Duration
}

func getConnectionAddress(ctx context.Context) string {
	peerInfo, ok := peer.FromContext(ctx)
	peerAddr := "unknown"
	if ok {
		peerAddr = peerInfo.Addr.String()
	}
	return peerAddr
}

// CreateCertificate handles an incoming certificate signing request (CSR). It does
// authentication and authorization. Upon validated, signs a certificate that:
// the SAN is the identity of the caller in authentication result.
// the subject public key is the public key in the CSR.
// the validity duration is the ValidityDuration in request, or default value if the given duration is invalid.
// it is signed by the CA signing key.
func (s *Server) CreateCertificate(ctx context.Context, request *pb.IstioCertificateRequest) (
	*pb.IstioCertificateResponse, error,
) {
	s.monitoring.CSR.Increment()
	caller := Authenticate(ctx, s.Authenticators)
	if caller == nil {
		s.monitoring.AuthnError.Increment()
		return nil, status.Error(codes.Unauthenticated, "request authenticate failure")
	}
	// TODO: Call authorizer.
	crMetadata := request.Metadata.GetFields()
	certSigner := crMetadata[security.CertSigner].GetStringValue()
	log.Debugf("cert signer from workload %s", certSigner)
	_, _, certChainBytes, rootCertBytes := s.ca.GetCAKeyCertBundle().GetAll()
	certOpts := ca.CertOpts{
		SubjectIDs: caller.Identities,
		TTL:        time.Duration(request.ValidityDuration) * time.Second,
		ForCA:      false,
		CertSigner: certSigner,
	}
	var signErr error
	var cert []byte
	var respCertChain []string
	if certSigner == "" {
		cert, signErr = s.ca.Sign([]byte(request.Csr), certOpts)
	} else {
		respCertChain, signErr = s.ca.SignWithCertChain([]byte(request.Csr), certOpts)
	}
	if signErr != nil {
		serverCaLog.Errorf("CSR signing error (%v)", signErr.Error())
		s.monitoring.GetCertSignError(signErr.(*caerror.Error).ErrorType()).Increment()
		return nil, status.Errorf(signErr.(*caerror.Error).HTTPErrorCode(), "CSR signing error (%v)", signErr.(*caerror.Error))
	}
	if certSigner == "" {
		respCertChain = []string{string(cert)}
		if len(certChainBytes) != 0 {
			respCertChain = append(respCertChain, string(certChainBytes))
		}
	}
	if len(rootCertBytes) != 0 {
		respCertChain = append(respCertChain, string(rootCertBytes))
	}
	response := &pb.IstioCertificateResponse{
		CertChain: respCertChain,
	}
	s.monitoring.Success.Increment()
	serverCaLog.Debug("CSR successfully signed.")
	return response, nil
}

func recordCertsExpiry(keyCertBundle *util.KeyCertBundle) {
	rootCertExpiry, err := keyCertBundle.ExtractRootCertExpiryTimestamp()
	if err != nil {
		serverCaLog.Errorf("failed to extract root cert expiry timestamp (error %v)", err)
	}
	rootCertExpiryTimestamp.Record(rootCertExpiry)

	if len(keyCertBundle.GetCertChainPem()) == 0 {
		return
	}

	certChainExpiry, err := keyCertBundle.ExtractCACertExpiryTimestamp()
	if err != nil {
		serverCaLog.Errorf("failed to extract CA cert expiry timestamp (error %v)", err)
	}
	certChainExpiryTimestamp.Record(certChainExpiry)
}

// Register registers a GRPC server on the specified port.
func (s *Server) Register(grpcServer *grpc.Server) {
	pb.RegisterIstioCertificateServiceServer(grpcServer, s)
}

// New creates a new instance of `IstioCAServiceServer`
func New(ca CertificateAuthority, ttl time.Duration,
	authenticators []security.Authenticator,
) (*Server, error) {
	certBundle := ca.GetCAKeyCertBundle()
	if len(certBundle.GetRootCertPem()) != 0 {
		recordCertsExpiry(certBundle)
	}
	server := &Server{
		Authenticators: authenticators,
		serverCertTTL:  ttl,
		ca:             ca,
		monitoring:     newMonitoringMetrics(),
	}
	return server, nil
}

// authenticate goes through a list of authenticators (provided client cert, k8s jwt, and ID token)
// and authenticates if one of them is valid.
func Authenticate(ctx context.Context, auth []security.Authenticator) *security.Caller {
	// TODO: apply different authenticators in specific order / according to configuration.
	var errMsg string
	for id, authn := range auth {
		u, err := authn.Authenticate(ctx)
		if err != nil {
			errMsg += fmt.Sprintf("Authenticator %s at index %d got error: %v. ", authn.AuthenticatorType(), id, err)
		}
		if u != nil && err == nil {
			serverCaLog.Debugf("Authentication successful through auth source %v", u.AuthSource)
			return u
		}
	}
	serverCaLog.Warnf("Authentication failed for %v: %s", getConnectionAddress(ctx), errMsg)
	return nil
}
