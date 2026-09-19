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
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"sync"
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

// dupRemovalLogOnce ensures we warn at most once per istiod process when a
// duplicate-certificate response is observed. This points operators at the
// root cause (typically a plug-in CA's cert-chain.pem that already includes
// the root cert) without flooding the log on every CSR.
var dupRemovalLogOnce sync.Once

// dedupCertChain removes byte-identical duplicate CERTIFICATE blocks from a
// PEM-encoded chain slice, comparing certs by SHA-256 of their DER body so
// whitespace differences cannot defeat the comparison. For any duplicate set
// the LAST occurrence is kept, so the spec invariant "the root cert is the
// last element" is preserved when the trailing root was the redundant copy.
//
// Fast path: if no duplicates exist, the input slice is returned unchanged
// with zero output allocations, so well-configured clusters pay nothing
// beyond N fingerprint hashes.
func dedupCertChain(chain []string) []string {
	if len(chain) <= 1 {
		return chain
	}

	// Fast-path pre-scan: detect duplicates without allocating any output.
	seen := make(map[string]struct{}, 4)
	hasDup := false
	for _, entry := range chain {
		rest := []byte(entry)
		for {
			block, remainder := pem.Decode(rest)
			if block == nil {
				break
			}
			if block.Type != "CERTIFICATE" {
				rest = remainder
				continue
			}
			fp := sha256.Sum256(block.Bytes)
			key := hex.EncodeToString(fp[:])
			if _, ok := seen[key]; ok {
				hasDup = true
				break
			}
			seen[key] = struct{}{}
			rest = remainder
		}
		if hasDup {
			break
		}
	}
	if !hasDup {
		return chain
	}

	// Slow-path rewrite: tail-to-head so "first wins on reversed" equals
	// "last wins on original".
	seen = make(map[string]struct{}, len(chain))
	out := make([]string, 0, len(chain))
	for i := len(chain) - 1; i >= 0; i-- {
		entry := chain[i]
		var kept []byte
		anyKept := false
		anyParsed := false
		rest := []byte(entry)
		for {
			block, remainder := pem.Decode(rest)
			if block == nil {
				break
			}
			anyParsed = true
			if block.Type != "CERTIFICATE" {
				kept = append(kept, pem.EncodeToMemory(block)...)
				anyKept = true
				rest = remainder
				continue
			}
			fp := sha256.Sum256(block.Bytes)
			key := hex.EncodeToString(fp[:])
			if _, dup := seen[key]; dup {
				rest = remainder
				continue
			}
			seen[key] = struct{}{}
			kept = append(kept, pem.EncodeToMemory(block)...)
			anyKept = true
			rest = remainder
		}
		if !anyParsed {
			out = append(out, entry)
			continue
		}
		if anyKept {
			out = append(out, string(kept))
		}
	}
	// out is in reverse order; restore the original ordering.
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

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
		sans = []string{impersonatedIdentity}
	}
	serverCaLog.Debugf("generating a certificate, sans: %v, requested ttl: %s", sans, time.Duration(request.ValidityDuration*int64(time.Second)))
	certSigner := crMetadata[security.CertSigner].GetStringValue()
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
	if certSigner == "" {
		cert, signErr = s.ca.Sign([]byte(request.Csr), certOpts)
	} else {
		serverCaLog.Debugf("signing CSR with cert chain")
		respCertChain, signErr = s.ca.SignWithCertChain([]byte(request.Csr), certOpts)
	}
	if signErr != nil {
		serverCaLog.Errorf("CSR signing error: %v", signErr.Error())
		s.monitoring.GetCertSignError(signErr.(*caerror.Error).ErrorType()).Increment()
		return nil, status.Errorf(signErr.(*caerror.Error).HTTPErrorCode(), "CSR signing error (%v)", signErr.(*caerror.Error))
	}
	if certSigner == "" {
		respCertChain = []string{string(cert)}
		if len(certChainBytes) != 0 {
			respCertChain = append(respCertChain, string(certChainBytes))
			serverCaLog.Debugf("Append cert chain to response, %s", string(certChainBytes))
		}
	}
	// expand `respCertChain` since each element might be a concatenated multi-cert PEM
	// the expanded structure (one cert per `string` in `certChain`) is specifically expected by `ztunnel`
	response := &pb.IstioCertificateResponse{}
	for _, pem := range respCertChain {
		for _, cert := range util.PemCertBytestoString([]byte(pem)) {
			// the trailing "\n" is added for backwards compatibility
			// there are ca clients (see pkg/test/framework/components/istio/ca.go) that would try to simply concatenate elements in the chain
			response.CertChain = append(response.CertChain, cert+"\n")
		}
	}
	// Per the spec: "... the root cert is the last element." so we do not want to flatten the root cert.
	// If we did, the client cannot distinguish the root.
	// A better API would put the root in a separate field entirely...
	if len(rootCertBytes) != 0 {
		response.CertChain = append(response.CertChain, string(rootCertBytes))
	}

	// Strip duplicate certificates from the response. The plug-in CA path
	// (Makefile-built cert-chain.pem ending in the root) plus the
	// unconditional rootCertBytes append above would otherwise emit the
	// root twice -- see istio/istio#39001. The helper preserves the
	// trailing root copy so the spec invariant "the root cert is the last
	// element" still holds.
	before := len(response.CertChain)
	response.CertChain = dedupCertChain(response.CertChain)
	if removed := before - len(response.CertChain); removed > 0 {
		dupRemovalLogOnce.Do(func() {
			serverCaLog.Warnf("removed %d duplicate certificate(s) from CSR response chain; "+
				"check that your plug-in CA's cert-chain.pem does not already include the root cert "+
				"(see istio/istio#39001). This warning is logged once per istiod process.", removed)
		})
	}

	serverCaLog.Debugf("Responding with cert chain, %q", response.CertChain)
	s.monitoring.Success.Increment()
	serverCaLog.Debugf("CSR successfully signed, sans %v.", sans)
	return response, nil
}

// RecordCertsExpiry updates the certificate-expiration related metrics given a new keycertbundle
func RecordCertsExpiry(keyCertBundle *util.KeyCertBundle) {
	// Expiry of the first root cert in trust bundle
	rootCertExpiry, err := keyCertBundle.ExtractRootCertExpiryTimestamp()
	if err != nil {
		serverCaLog.Errorf("failed to extract root cert expiry timestamp (error %v)", err)
	} else {
		rootCertExpiryTimestamp.Record(float64(rootCertExpiry.Unix()))
		rootCertExpirySeconds.ValueFrom(func() float64 { return time.Until(rootCertExpiry).Seconds() })
	}

	if len(keyCertBundle.GetCertChainPem()) == 0 {
		return
	}

	certChainExpiry, err := keyCertBundle.ExtractCACertExpiryTimestamp()
	if err != nil {
		serverCaLog.Errorf("failed to extract CA cert expiry timestamp (error %v)", err)
	} else {
		certChainExpiryTimestamp.Record(float64(certChainExpiry.Unix()))
		certChainExpirySeconds.ValueFrom(func() float64 { return time.Until(certChainExpiry).Seconds() })
	}
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
		RecordCertsExpiry(certBundle)
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
