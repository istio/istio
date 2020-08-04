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
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes"

	"istio.io/pkg/log"

	pb "istio.io/api/security/v1alpha1"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/server/ca/authenticate"
)

// Config for Vault prototyping purpose
const (
	jwtPath              = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	caCertPath           = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	certExpirationBuffer = time.Minute
)

var serverCaLog = log.RegisterScope("serverca", "Citadel server log", 0)

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
	Authenticators []authenticate.Authenticator
	hostnames      []string
	ca             CertificateAuthority
	serverCertTTL  time.Duration
	certificate    *tls.Certificate
	port           int
	forCA          bool
	grpcServer     *grpc.Server
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
	*pb.IstioCertificateResponse, error) {
	s.monitoring.CSR.Increment()
	caller := s.authenticate(ctx)
	if caller == nil {
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
	s.monitoring.Success.Increment()
	serverCaLog.Debug("CSR successfully signed.")

	return response, nil
}

func recordCertsExpiry(keyCertBundle util.KeyCertBundle) {
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
	hostlist []string, port int, trustDomain string, sdsEnabled bool, jwtPolicy, clusterID string) (*Server, error) {
	return NewWithGRPC(nil, ca, ttl, forCA, hostlist, port, trustDomain, sdsEnabled, jwtPolicy, clusterID, nil, nil)
}

// New creates a new instance of `IstioCAServiceServer`, running inside an existing gRPC server.
func NewWithGRPC(grpc *grpc.Server, ca CertificateAuthority, ttl time.Duration, forCA bool,
	hostlist []string, port int, trustDomain string, sdsEnabled bool, jwtPolicy, clusterID string,
	kubeClient kubernetes.Interface,
	remoteKubeClientGetter authenticate.RemoteKubeClientGetter) (*Server, error) {

	if len(hostlist) == 0 {
		return nil, fmt.Errorf("failed to create grpc server hostlist empty")
	}
	// Notice that the order of authenticators matters, since at runtime
	// authenticators are activated sequentially and the first successful attempt
	// is used as the authentication result.
	authenticators := []authenticate.Authenticator{&authenticate.ClientCertAuthenticator{}}
	serverCaLog.Info("added client certificate authenticator")

	// Only add k8s jwt authenticator if SDS is enabled.
	if sdsEnabled {
		authenticator := authenticate.NewKubeJWTAuthenticator(kubeClient, clusterID, remoteKubeClientGetter,
			trustDomain, jwtPolicy)
		authenticators = append(authenticators, authenticator)
		serverCaLog.Info("added K8s JWT authenticator")
	}

	recordCertsExpiry(ca.GetCAKeyCertBundle())

	server := &Server{
		Authenticators: authenticators,
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

	bundle := s.ca.GetCAKeyCertBundle()
	if bundle != nil {
		// cert bundles can have errors (e.g. missing SAN)
		// that do not matter for getting the encryption type
		_, privKey, _, _ := bundle.GetAll()
		if util.IsSupportedECPrivateKey(privKey) {
			opts = util.CertOptions{
				ECSigAlg: util.EcdsaSigAlg,
			}
		}
	}

	// TODO the user can specify algorithm to generate for CSRs independent of CA certificate
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
	serverCaLog.Warnf("Authentication failed for %v: %s", getConnectionAddress(ctx), errMsg)
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
