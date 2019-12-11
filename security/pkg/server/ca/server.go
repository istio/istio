// Copyright 2017 Istio Authors
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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"time"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/registry"
	"istio.io/istio/security/pkg/server/ca/authenticate"
	pb "istio.io/istio/security/proto"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

// Config for Vault prototyping purpose
const (
	jwtPath              = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	caCertPath           = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	k8sAPIServerURL      = "https://kubernetes.default.svc/apis/authentication.k8s.io/v1/tokenreviews"
	certExpirationBuffer = time.Minute
)

var serverCaLog = log.RegisterScope("serverCaLog", "Citadel server log", 0)

type authenticator interface {
	Authenticate(ctx context.Context) (*authenticate.Caller, error)
	AuthenticatorType() string
}

// CertificateAuthority contains methods to be supported by a CA.
type CertificateAuthority interface {
	// Sign generates a certificate for a workload or CA, from the given CSR and TTL.
	// TODO(myidpt): simplify this interface and pass a struct with cert field values instead.
	Sign(csrPEM []byte, subjectIDs []string, ttl time.Duration, forCA bool) ([]byte, error)
	// SignWithCertChain is similar to Sign but returns the leaf cert and the entire cert chain.
	SignWithCertChain(csrPEM []byte, subjectIDs []string, ttl time.Duration, forCA bool) ([]byte, error)
	// GetCAKeyCertBundle returns the KeyCertBundle used by CA.
	GetCAKeyCertBundle() util.KeyCertBundle
}

// Server implements IstioCAService and IstioCertificateService and provides the services on the
// specified port.
type Server struct {
	monitoring     monitoringMetrics
	Authenticators []authenticator
	hostnames      []string
	authorizer     authorizer
	ca             CertificateAuthority
	serverCertTTL  time.Duration
	certificate    *tls.Certificate
	port           int
	forCA          bool
	grpcServer     *grpc.Server
}

// CreateCertificate handles an incoming certificate signing request (CSR). It does
// authentication and authorization. Upon validated, signs a certificate that:
// the SAN is the identity of the caller in authentication result.
// the subject public key is the public key in the CSR.
// the validity duration is the ValidityDuration in request, or default value if the given duration is invalid.
// it is signed by the CA signing key.
func (s *Server) CreateCertificate(ctx context.Context, request *pb.IstioCertificateRequest) (
	*pb.IstioCertificateResponse, error) {
	caller := s.authenticate(ctx)
	if caller == nil {
		serverCaLog.Warn("request authentication failure")
		s.monitoring.AuthnError.Increment()
		return nil, status.Error(codes.Unauthenticated, "request authenticate failure")
	}

	// TODO: Call authorizer.

	_, _, certChainBytes, rootCertBytes := s.ca.GetCAKeyCertBundle().GetAll()
	cert, signErr := s.ca.Sign(
		[]byte(request.Csr), caller.Identities, time.Duration(request.ValidityDuration)*time.Second, false)
	if signErr != nil {
		serverCaLog.Errorf("CSR signing error (%v)", signErr.Error())
		s.monitoring.GetCertSignError(signErr.(*caerror.Error).ErrorType()).Increment()
		return nil, status.Errorf(signErr.(*caerror.Error).HTTPErrorCode(), "CSR signing error (%v)", signErr.(*caerror.Error))
	}
	respCertChain := []string{string(cert)}
	if len(certChainBytes) != 0 {
		respCertChain = append(respCertChain, string(certChainBytes))
	}
	respCertChain = append(respCertChain, string(rootCertBytes))
	response := &pb.IstioCertificateResponse{
		CertChain: respCertChain,
	}
	serverCaLog.Debug("CSR successfully signed.")

	return response, nil
}

// extractRootCertExpiryTimestamp returns the unix timestamp when the root becomes expires.
func extractRootCertExpiryTimestamp(ca CertificateAuthority) float64 {
	rb := ca.GetCAKeyCertBundle().GetRootCertPem()
	cert, err := util.ParsePemEncodedCertificate(rb)
	if err != nil {
		serverCaLog.Errorf("Failed to parse the root cert: %v", err)
		return -1
	}
	end := cert.NotAfter
	if end.Before(time.Now()) {
		serverCaLog.Errorf("Expired Citadel Root found, x509.NotAfter %v, please transit your root", end)
	}
	return float64(end.Unix())
}

// HandleCSR handles an incoming certificate signing request (CSR). It does
// proper validation (e.g. authentication) and upon validated, signs the CSR
// and returns the resulting certificate. If not approved, reason for refusal
// to sign is returned as part of the response object.
// [TODO](myidpt): Deprecate this function.
func (s *Server) HandleCSR(ctx context.Context, request *pb.CsrRequest) (*pb.CsrResponse, error) {
	s.monitoring.CSR.Increment()
	caller := s.authenticate(ctx)
	if caller == nil || len(caller.Identities) == 0 {
		serverCaLog.Warn("request authentication failure, no caller identity")
		s.monitoring.AuthnError.Increment()
		return nil, status.Error(codes.Unauthenticated, "request authenticate failure, no caller identity")
	}

	csr, err := util.ParsePemEncodedCSR(request.CsrPem)
	if err != nil {
		serverCaLog.Warnf("CSR Pem parsing error (error %v)", err)
		s.monitoring.CSRError.Increment()
		return nil, status.Errorf(codes.InvalidArgument, "CSR parsing error (%v)", err)
	}

	_, err = util.ExtractIDs(csr.Extensions)
	if err != nil {
		serverCaLog.Warnf("CSR identity extraction error (%v)", err)
		s.monitoring.IDExtractionError.Increment()
		return nil, status.Errorf(codes.InvalidArgument, "CSR identity extraction error (%v)", err)
	}

	// TODO: Call authorizer.

	_, _, certChainBytes, _ := s.ca.GetCAKeyCertBundle().GetAll()
	cert, signErr := s.ca.Sign(
		request.CsrPem, caller.Identities, time.Duration(request.RequestedTtlMinutes)*time.Minute, s.forCA)
	if signErr != nil {
		serverCaLog.Errorf("CSR signing error (%v)", signErr.Error())
		s.monitoring.GetCertSignError(signErr.(*caerror.Error).ErrorType()).Increment()
		return nil, status.Errorf(codes.Internal, "CSR signing error (%v)", signErr.(*caerror.Error))
	}

	response := &pb.CsrResponse{
		IsApproved: true,
		SignedCert: cert,
		CertChain:  certChainBytes,
	}
	serverCaLog.Debug("CSR successfully signed.")
	s.monitoring.Success.Increment()

	return response, nil
}

// Run starts a GRPC server on the specified port.
func (s *Server) Run() error {
	grpcServer := s.grpcServer
	var listener net.Listener
	var err error

	if grpcServer == nil {
		listener, err = net.Listen("tcp", fmt.Sprintf(":%d", s.port))
		if err != nil {
			return fmt.Errorf("cannot listen on port %d (error: %v)", s.port, err)
		}

		var grpcOptions []grpc.ServerOption
		grpcOptions = append(grpcOptions, s.createTLSServerOption(), grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor))

		grpcServer = grpc.NewServer(grpcOptions...)
	}
	pb.RegisterIstioCAServiceServer(grpcServer, s)
	pb.RegisterIstioCertificateServiceServer(grpcServer, s)

	grpc_prometheus.EnableHandlingTimeHistogram()
	grpc_prometheus.Register(grpcServer)

	if listener != nil {
		// grpcServer.Serve() is a blocking call, so run it in a goroutine.
		go func() {
			serverCaLog.Infof("Starting GRPC server on port %d", s.port)

			err := grpcServer.Serve(listener)

			// grpcServer.Serve() always returns a non-nil error.
			serverCaLog.Warnf("GRPC server returns an error: %v", err)
		}()
	}

	return nil
}

// New creates a new instance of `IstioCAServiceServer`.
func New(ca CertificateAuthority, ttl time.Duration, forCA bool,
	hostlist []string, port int, trustDomain string, sdsEnabled bool) (*Server, error) {
	return NewWithGRPC(nil, ca, ttl, forCA, hostlist, port, trustDomain, sdsEnabled)
}

// New creates a new instance of `IstioCAServiceServer`, running inside an existing gRPC server.
func NewWithGRPC(grpc *grpc.Server, ca CertificateAuthority, ttl time.Duration, forCA bool,
	hostlist []string, port int, trustDomain string, sdsEnabled bool) (*Server, error) {

	if len(hostlist) == 0 {
		return nil, fmt.Errorf("failed to create grpc server hostlist empty")
	}
	// Notice that the order of authenticators matters, since at runtime
	// authenticators are activated sequentially and the first successful attempt
	// is used as the authentication result.
	authenticators := []authenticator{&authenticate.ClientCertAuthenticator{}}
	serverCaLog.Info("added client certificate authenticator")

	// Only add k8s jwt authenticator if SDS is enabled.
	if sdsEnabled {
		authenticator, err := authenticate.NewKubeJWTAuthenticator(k8sAPIServerURL, caCertPath, jwtPath,
			trustDomain)
		if err == nil {
			authenticators = append(authenticators, authenticator)
			serverCaLog.Info("added K8s JWT authenticator")
		} else {
			serverCaLog.Warnf("failed to add JWT authenticator: %v", err)
		}
	}

	// Temporarily disable ID token authenticator by resetting the hostlist.
	// [TODO](myidpt): enable ID token authenticator when the CSR API authz can work correctly.
	hostlistForJwtAuth := make([]string, 0)
	for _, host := range hostlistForJwtAuth {
		aud := fmt.Sprintf("grpc://%s:%d", host, port)
		if jwtAuthenticator, err := authenticate.NewIDTokenAuthenticator(aud); err != nil {
			serverCaLog.Errorf("failed to create JWT authenticator (error %v)", err)
		} else {
			authenticators = append(authenticators, jwtAuthenticator)
			serverCaLog.Infof("added general JWT authenticator")
		}
	}

	version.Info.RecordComponentBuildTag("citadel")
	rootCertExpiryTimestamp.Record(extractRootCertExpiryTimestamp(ca))

	server := &Server{
		Authenticators: authenticators,
		authorizer:     &registryAuthorizor{registry.GetIdentityRegistry()},
		serverCertTTL:  ttl,
		ca:             ca,
		hostnames:      hostlist,
		forCA:          forCA,
		port:           port,
		grpcServer:     grpc,
		monitoring:     newMonitoringMetrics(),
	}
	return server, nil
}

func (s *Server) createTLSServerOption() grpc.ServerOption {
	cp := x509.NewCertPool()
	rootCertBytes := s.ca.GetCAKeyCertBundle().GetRootCertPem()
	cp.AppendCertsFromPEM(rootCertBytes)

	config := &tls.Config{
		ClientCAs:  cp,
		ClientAuth: tls.VerifyClientCertIfGiven,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			if s.certificate == nil || shouldRefresh(s.certificate) {
				// Apply new certificate if there isn't one yet, or the one has become invalid.
				newCert, err := s.getServerCertificate()
				if err != nil {
					return nil, fmt.Errorf("failed to apply TLS server certificate (%v)", err)
				}
				s.certificate = newCert
			}
			return s.certificate, nil
		},
	}
	return grpc.Creds(credentials.NewTLS(config))
}

// getServerCertificate returns a valid server TLS certificate and the intermediate CA certificates,
// signed by the current CA root.
func (s *Server) getServerCertificate() (*tls.Certificate, error) {
	opts := util.CertOptions{
		RSAKeySize: 2048,
	}

	csrPEM, privPEM, err := util.GenCSR(opts)
	if err != nil {
		return nil, err
	}

	certPEM, signErr := s.ca.SignWithCertChain(csrPEM, s.hostnames, s.serverCertTTL, false)
	if signErr != nil {
		return nil, signErr.(*caerror.Error)
	}

	cert, err := tls.X509KeyPair(certPEM, privPEM)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// authenticate goes through a list of authenticators (provided client cert, k8s jwt, and ID token)
// and authenticates if one of them is valid.
func (s *Server) authenticate(ctx context.Context) *authenticate.Caller {
	// TODO: apply different authenticators in specific order / according to configuration.
	var errMsg string
	for id, authn := range s.Authenticators {
		u, err := authn.Authenticate(ctx)
		if err != nil {
			errMsg += fmt.Sprintf("Authenticator %s at index %d got error: %v. ", authn.AuthenticatorType(), id, err)
		}
		if u != nil && err == nil {
			serverCaLog.Debugf("Authentication successful through auth source %v", u.AuthSource)
			return u
		}
	}
	serverCaLog.Warnf("Authentication failed: %s", errMsg)
	return nil
}

// shouldRefresh indicates whether the given certificate should be refreshed.
func shouldRefresh(cert *tls.Certificate) bool {
	// Check whether there is a valid leaf certificate.
	leaf := cert.Leaf
	if leaf == nil {
		return true
	}

	// Check whether the leaf certificate is about to expire.
	return leaf.NotAfter.Add(-certExpirationBuffer).Before(time.Now())
}
