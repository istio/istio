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
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/pki/ca"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
)

var serverCaLog = log.RegisterScope("serverca", "Citadel server log")

// CertificateAuthority contains methods to be supported by a CA.
type CertificateAuthority interface {
	// Sign generates a certificate for a workload or CA, from the given CSR and cert opts.
	// The Istio signer returns the leaf certificate only, result must be concatenated with the intermediaries.
	// The RA (with K8_SIGNER) returns the full chain, there are no separate intermediaries.
	Sign(csrPEM []byte, opts ca.CertOpts) ([]byte, error)

	// SignWithCertChain is similar to Sign but returns the leaf cert and the entire cert chain, and is used
	// if the user supplies a custom signer header.
	//
	// IstioCA returns an array of 1 string, with the full chain (tls.crt format)
	// KubernetesRA can return a similar array of 1 string, or add a second element with the
	//  extracted root CA, as the last element in the signing chain if no roots
	//  are explicitly configured.
	//
	// TODO: rename to "SignWithCustomSigner"
	SignWithCertChain(csrPEM []byte, opts ca.CertOpts) ([]string, error)

	// GetCAKeyCertBundle returns the KeyCertBundle used by CA.
	// The CA service is using the roots and (optional) intermediates.
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

	nodeAuthorizer *MulticlusterNodeAuthorizor
}

type SaNode struct {
	ServiceAccount types.NamespacedName
	Node           string
}

func (s SaNode) String() string {
	return s.Node + "/" + s.ServiceAccount.String()
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
	caller, err := security.Authenticate(ctx, s.Authenticators)
	if caller == nil || err != nil {
		s.monitoring.AuthnError.Increment()
		return nil, status.Error(codes.Unauthenticated, "request authenticate failure")
	}

	serverCaLog := serverCaLog.WithLabels("client", security.GetConnectionAddress(ctx))
	// By default, we will use the callers identity for the certificate
	sans := caller.Identities
	crMetadata := request.Metadata.GetFields()
	impersonatedIdentity := crMetadata[security.ImpersonatedIdentity].GetStringValue()
	if impersonatedIdentity != "" {
		// This is used by ztunnel to create a cert for a pod on same node.
		serverCaLog.Debugf("impersonated identity: %s", impersonatedIdentity)
		// If there is an impersonated identity, we will override to use that identity (only single value
		// supported), if the real caller is authorized.
		if s.nodeAuthorizer == nil {
			s.monitoring.AuthnError.Increment()
			// Return an opaque error (for security purposes) but log the full reason
			serverCaLog.Warnf("impersonation not allowed, as node authorizer (CA_TRUSTED_NODE_ACCOUNTS) is not configured")
			return nil, status.Error(codes.Unauthenticated, "request impersonation authentication failure")

		}
		if err := s.nodeAuthorizer.authenticateImpersonation(ctx, caller.KubernetesInfo, impersonatedIdentity); err != nil {
			s.monitoring.AuthnError.Increment()
			// Return an opaque error (for security purposes) but log the full reason
			serverCaLog.Warnf("impersonation failed for identity %s, error: %v", impersonatedIdentity, err)
			return nil, status.Error(codes.Unauthenticated, "request impersonation authentication failure")
		}
		// Node is authorized to impersonate; overwrite the SAN to the impersonated identity.
		// TODO(costin) extract it from the CSR - and verify it. For IstioCA - we ignore the SANs in the CSR, and K8S RA
		// is using them - but checks the SANs.
		// RA will not accept a different CSR, we don't have the key - so original SAN needs to be proper and is passed to the
		// real CA.
		sans = []string{impersonatedIdentity}
	}
	serverCaLog.Debugf("generating a certificate, sans: %v, requested ttl: %s", sans, time.Duration(request.ValidityDuration*int64(time.Second)))

	// "CertSigner" metadata is used together with the CERT_SIGNER_DOMAIN.
	certSigner := crMetadata[security.CertSigner].GetStringValue()

	// rootCertBytes may be a list of PEM certificates.
	// certChainBytes may be empty if it is the self-signed cert without intermediates.
	_, _, certChainBytes, rootCertBytes := s.ca.GetCAKeyCertBundle().GetAll()
	certOpts := ca.CertOpts{
		SubjectIDs: sans,
		TTL:        time.Duration(request.ValidityDuration) * time.Second,
		ForCA:      false,
		CertSigner: certSigner,
	}
	var signErr error
	var cert []byte
	var respCertChain []string

	// Normal signing - using Istio CA or RA with a fixed signer name (K8S_SIGNER)
	if certSigner == "" {
		// Istio CA with generated certs: no intermediaryes, sign returns the leaf, return roots as 2nd element
		// Istio CA with cacerts ( intermediaries) - sign return the leaf, we add certChainBytes as second element and roots as 3rd.
		// RA with K8S_SIGNER: Sign returns the full chain - return 1st element as leaf+intermediaries, 2nd is roots.
		cert, signErr = s.ca.Sign([]byte(request.Csr), certOpts)
		if signErr != nil {
			serverCaLog.Errorf("CSR signing error: %v", signErr.Error())
			s.monitoring.GetCertSignError(signErr.(*caerror.Error).ErrorType()).Increment()
			return nil, status.Errorf(signErr.(*caerror.Error).HTTPErrorCode(), "CSR signing error (%v)", signErr.(*caerror.Error))
		}
		respCertChain = []string{string(cert)}
		if len(certChainBytes) != 0 {
			respCertChain = append(respCertChain, string(certChainBytes))
			serverCaLog.Debugf("Append cert chain to response, %s", string(certChainBytes))
		}
		if len(rootCertBytes) != 0 {
			respCertChain = append(respCertChain, string(rootCertBytes))
		} else {
			// The client normally expects the last element in the response to be the root certs.
			// This is likely a bug.
			log.Warnf("Returned certificate without roots %s", caller.Identities)
		}
	} else {
		// Signing with a user-supplied signer name, combined with a Istiod configured CERT_SIGNER_DOMAIN

		// TODO(costin): this concatenates all PEMs in one response, doesn't seem to match the contract
		// of the API ( an array of certs ) or what ztunnel is using. Not clear when this option is used.
		// Seems to only be used with KubernetesRA (which returns concatenated chain) and if node meta is
		// set - will not be the case with ztunnel.
		serverCaLog.Debugf("signing CSR with cert chain")
		respCertChain, signErr = s.ca.SignWithCertChain([]byte(request.Csr), certOpts)
		if signErr != nil {
			serverCaLog.Errorf("CSR signing error: %v", signErr.Error())
			s.monitoring.GetCertSignError(signErr.(*caerror.Error).ErrorType()).Increment()
			return nil, status.Errorf(signErr.(*caerror.Error).HTTPErrorCode(), "CSR signing error (%v)", signErr.(*caerror.Error))
		}
		// If rootCertBytes is not set (not clear in which case - the roots are required for Istiod mTLS to work)
		// the SignWithCertChain will actually add a root - either from mesh config or the last element in the chain.
		// It is not clear how the roots from MeshConfig will be handled if Istiod root cert chains are set properly.
		// This is likely a bug.
		if len(rootCertBytes) != 0 {
			respCertChain = append(respCertChain, string(rootCertBytes))
		}
	}

	response := &pb.IstioCertificateResponse{
		CertChain: respCertChain,
	}
	s.monitoring.Success.Increment()
	serverCaLog.Debugf("CSR successfully signed, sans %v.", caller.Identities)
	// For audit - issuing a cert is an important operation.
	serverCaLog.WithLabels("identities", caller.Identities, "authSource", caller.AuthSource, "k8sInfo", caller.KubernetesInfo,
		"sans", sans).Info("CertificateIssued")
	return response, nil
}

func recordCertsExpiry(keyCertBundle *util.KeyCertBundle) {
	rootCertExpiry, err := keyCertBundle.ExtractRootCertExpiryTimestamp()
	if err != nil {
		serverCaLog.Errorf("failed to extract root cert expiry timestamp (error %v)", err)
	}
	rootCertExpiryTimestamp.Record(rootCertExpiry)

	rootCertPem, err := util.ParsePemEncodedCertificate(keyCertBundle.GetRootCertPem())
	if err != nil {
		serverCaLog.Errorf("failed to parse the root cert: %v", err)
	}
	rootCertExpirySeconds.ValueFrom(func() float64 { return time.Until(rootCertPem.NotAfter).Seconds() })

	if len(keyCertBundle.GetCertChainPem()) == 0 {
		return
	}

	certChainExpiry, err := keyCertBundle.ExtractCACertExpiryTimestamp()
	if err != nil {
		serverCaLog.Errorf("failed to extract CA cert expiry timestamp (error %v)", err)
	}
	certChainExpiryTimestamp.Record(certChainExpiry)

	certChainPem, err := util.ParsePemEncodedCertificate(keyCertBundle.GetCertChainPem())
	if err != nil {
		serverCaLog.Errorf("failed to parse the cert chain: %v", err)
	}
	certChainExpirySeconds.ValueFrom(func() float64 { return time.Until(certChainPem.NotAfter).Seconds() })
}

// Register registers a GRPC server on the specified port.
func (s *Server) Register(grpcServer *grpc.Server) {
	pb.RegisterIstioCertificateServiceServer(grpcServer, s)
}

// New creates a new instance of `IstioCAServiceServer`
func New(
	ca CertificateAuthority,
	ttl time.Duration,
	authenticators []security.Authenticator,
	controller multicluster.ComponentBuilder,
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

	if len(features.CATrustedNodeAccounts) > 0 {
		// TODO: do we need some way to delayed readiness until this is synced? Probably
		// Worst case is we deny some requests though which are retried
		server.nodeAuthorizer = NewMulticlusterNodeAuthenticator(features.CATrustedNodeAccounts, controller)
	}
	return server, nil
}
