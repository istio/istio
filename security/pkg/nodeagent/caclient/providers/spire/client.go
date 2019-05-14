// Copyright 2019 Istio Authors
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

package spire

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	spiffeTls "github.com/spiffe/go-spiffe/tls"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	clientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	spireIntegration "istio.io/istio/security/proto/providers/spire"
	"istio.io/pkg/log"
)

const (
	serverID         = "spiffe://%s/spire/server"
	spiffeIDTemplate = "spiffe://%s"
)

type spireClient struct {
	serverAddr  string
	trustDomain string

	client       spireIntegration.NodeClient
	clientConn   *grpc.ClientConn
	latestBundle string
	mutex        *sync.Mutex
}

// NewSpireClient create a CA client for Spire.
func NewSpireClient(serverAddr string, tlsRootCert []byte, trustDomain string) (clientInterface.Client, error) {
	c := &spireClient{
		serverAddr:   serverAddr,
		trustDomain:  trustDomain,
		latestBundle: string(tlsRootCert),
		mutex:        &sync.Mutex{},
	}

	err := c.createNodeClient(tlsRootCert)
	if err != nil {
		return nil, err
	}

	return c, nil
}

type IstioAttestableData struct {
	Token string `json:"token"`
}

// CSRSign calls Spire server to sign a CSR.
func (c *spireClient) CSRSign(ctx context.Context, csrPEM []byte, token string,
	certValidTTLInSec int64) ([]string, error) {

	attestableData := &IstioAttestableData{
		Token: token,
	}

	data, err := json.Marshal(attestableData)
	if err != nil {
		log.Errorf("Failed to parse attestable SPIRE data %v", err)
		return nil, errors.New("failed to parse attestable SPIRE data")
	}
	attestationData := &spireIntegration.AttestationData{
		Data: data,
		Type: "istio",
	}

	certRequest, err := csrFromPEM(csrPEM)
	if err != nil {
		log.Errorf("Could not decode csr to be signed by SPIRE: %v", err)
		return nil, errors.New("could not decode csr to be signed by SPIRE")
	}

	if len(certRequest.URIs) != 1 {
		return nil, errors.New("signing certificate must contain exactly one SPIFFE ID")
	}

	certID := certRequest.URIs[0]
	if err := c.validateWorkloadID(certID); err != nil {
		log.Errorf("Spiffe id from CSR is not valid: %v", err)
		return nil, errors.New("spiffe id from CSR is not valid")
	}

	attestReq := &spireIntegration.AttestRequest{
		Csr:             certRequest.Raw,
		AttestationData: attestationData,
	}

	// cancel context to be sure stream is closed
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// create an attestation stream
	attestStream, err := c.client.Attest(ctx)
	if err != nil {
		log.Errorf("Failed opening stream to attestating SPIRE server: %v", err)
		return nil, errors.New("failed opening stream to attestating SPIRE server")
	}

	if err := attestStream.Send(attestReq); err != nil {
		log.Errorf("Not able to send attestation request to SPIRE server: %v", err)
		return nil, fmt.Errorf("not able to send attestation request to SPIRE server")
	}

	attestResp, err := attestStream.Recv()
	if err != nil {
		return nil, fmt.Errorf("attesting to SPIRE server: %v", err)
	}

	if len(attestResp.SvidUpdate.Svids) == 0 {
		return nil, fmt.Errorf("no svid returned from attestation call to SPIRE server")
	}

	svid, ok := attestResp.SvidUpdate.Svids[certID.String()]
	if !ok {
		log.Errorf("SPIRE response does not contains a SVID for %v", certID)
		return nil, fmt.Errorf("SPIRE response does not contains a SVID for %v", certID)
	}

	// svid cert chain can contains multiple certificates
	certificates, err := x509.ParseCertificates(svid.CertChain)
	if err != nil {
		log.Errorf("Failed to parse svid certificates from SPIRE: %v", err)
		return nil, fmt.Errorf("failed to parse svid from SPIRE")
	}

	// Get bundle for trust domain
	bundle, ok := attestResp.SvidUpdate.Bundles[fmt.Sprintf(spiffeIDTemplate, certID.Host)]
	if !ok {
		log.Errorf("No bundle returned from SPIRE")
		return nil, fmt.Errorf("no bundle returned from SPIRE")
	}

	var bundles []*x509.Certificate
	for _, ca := range bundle.RootCas {
		c, err := x509.ParseCertificate(ca.DerBytes)
		if err != nil {
			log.Error("not able to parse certificate from SPIRE bundle response")
		} else {
			bundles = append(bundles, c)
		}

	}

	certChain, err := c.buildCertChain(certificates, bundles)
	if err != nil {
		return nil, err
	}

	return certChain, nil
}

// buildCertChain creates a certificate chain based on svid response
func (c *spireClient) buildCertChain(certificates, bundles []*x509.Certificate) ([]string, error) {
	certsMap := make(map[string]bool)
	var certChain []string

	// add certificates
	for _, cert := range certificates {
		certPEM := certChainPEM(cert.Raw)

		certsMap[certPEM] = true
		certChain = append(certChain, certPEM)
	}

	var latestBundle string
	var rootCerts string

	r, err := buildFullChain(certificates, bundles)
	if err != nil {
		log.Errorf("Failed to build certificate chain from SPIRE server: %v", err)
		return nil, errors.New("failed to build certificate chain from SPIRE server")
	}

	// Istio considers the last element in the returned chain the “root” certificate that is used to validate peer certificates.
	// Lump all of the certificates in the chain of trust that belong to the trust bundle returned by SPIRE into a single entry to be used as the “root”.
	for _, cert := range r {
		caPEM := certChainPEM(cert.Raw)
		if !certsMap[caPEM] {
			rootCerts += caPEM
		}
	}
	certChain = append(certChain, rootCerts)

	for _, ca := range bundles {
		latestBundle += certChainPEM(ca.Raw)
	}

	if err = c.setLatestBundle(latestBundle); err != nil {
		// fail only when recreating client, it will log it and retry in next call.
		log.Errorf("Not able to update bootstrap certificate for SPIRE server: %v", err)
	}

	return certChain, nil
}

// setLatestBundle sets the bundle used to verify connections to SPIRE.
// If the bundle has changed, the SPIRE Node API client is reinitialized to pick up the change.
func (c *spireClient) setLatestBundle(bundleData string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.latestBundle != bundleData {
		err := c.createNodeClient([]byte(bundleData))
		if err != nil {
			return err
		}

		c.latestBundle = bundleData
	}

	return nil
}

// serverConn create a client connection to spiffe server
func (c *spireClient) serverConn(bundle []byte) (*grpc.ClientConn, error) {
	if len(bundle) == 0 {
		return nil, errors.New("bootstrap certificate is required for SPIRE")
	}

	pool := x509.NewCertPool()
	ok := pool.AppendCertsFromPEM(bundle)
	if !ok {
		return nil, errors.New("failed to append bundle to the certificate pool")
	}

	spiffePeer := &spiffeTls.TLSPeer{
		SpiffeIDs:  []string{fmt.Sprintf(serverID, c.trustDomain)},
		TrustRoots: pool,
	}

	tlsConfig := spiffePeer.NewTLSConfig([]tls.Certificate{})

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	}

	return grpc.Dial(c.serverAddr, opts...)
}

// createNodeClient dials the SPIRE server and initializes a gRPC client to the Node API.
// It authenticates the SPIRE server using the provided bundle.
func (c *spireClient) createNodeClient(bundle []byte) error {
	conn, err := c.serverConn(bundle)
	if err != nil {
		log.Errorf("Failed to connect to SPIRE server %s: %v", c.serverAddr, err)
		return fmt.Errorf("failed to connect to SPIRE server %s", c.serverAddr)
	}

	if c.clientConn != nil {
		err := c.clientConn.Close()
		if err != nil {
			log.Errorf("Failed to close client connection: %v", err)
		}
	}

	c.clientConn = conn
	c.client = spireIntegration.NewNodeClient(conn)

	return nil
}

// validateWorkloadID validates that the ID is a valid workload SPIFFE ID within the configured trust domain.
func (c *spireClient) validateWorkloadID(id *url.URL) error {
	switch {
	case !strings.EqualFold(id.Scheme, "spiffe"):
		return errors.New("invalid scheme")
	case id.User != nil:
		return errors.New("user info is not allowed")
	case id.Host == "":
		return errors.New("trust domain is empty")
	case id.Port() != "":
		return errors.New("port is not allowed")
	case id.Fragment != "":
		return errors.New("fragment is not allowed")
	case id.RawQuery != "":
		return errors.New("query is not allowed")
	case id.Host != c.trustDomain:
		return fmt.Errorf("%q does not belong to trust domain %q", id, c.trustDomain)
	case id.Path == "":
		return errors.New("path must not be empty")
	case strings.HasPrefix(id.Path, "/spire/"):
		// SPIRE reserves identities under /spire within trust domains
		return errors.New("the path cannot be part of the SPIRE reserved namespace")
	}

	return nil
}

// buildFullChain returns a verified chain of trust from the certChain back to certificates present in trustBundle.
// If more than one chain of trust can be formed. for example when trustBundle contains intermediate certificates
// signed by other certificates within trustBundle, the most complete chain is returned (i.e. the chain that goes back the farthest).
func buildFullChain(certChain, trustBundle []*x509.Certificate) ([]*x509.Certificate, error) {
	if len(certChain) == 0 {
		return nil, errors.New("empty certificate chain")
	}
	intermediates := x509.NewCertPool()
	for _, intermediate := range certChain[1:] {
		intermediates.AddCert(intermediate)
	}
	roots := x509.NewCertPool()
	for _, root := range trustBundle {
		roots.AddCert(root)
	}

	chains, err := certChain[0].Verify(x509.VerifyOptions{
		Intermediates: intermediates,
		Roots:         roots,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return nil, err
	}

	// The longest verified chain should be the most "complete". For example, if we have a chain of trust:
	//  D -> C -> B -> A
	// And the cert chain contains:
	// [D,C]
	// And the trust bundle contains:
	// [B,A]
	// Then `Verify` will return two chains since it can form two chains back to certs in the trust bundle:
	// [D,C,B] and [D,C,B,A]
	//
	// [D,C,B,A] represents a more complete chain and is necessary to convey to workloads so they can verify identities signed by A
	var longestChain []*x509.Certificate
	for _, chain := range chains {
		if len(longestChain) < len(chain) {
			longestChain = chain
		}
	}

	return longestChain, nil
}

// certChainPEM parse provided certificate to pem format
func certChainPEM(chain []byte) string {
	b := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: chain,
	}
	return string(pem.EncodeToMemory(b))
}

// csrFromPEM parses the PEM encoded DER bytes into a CSR struct
func csrFromPEM(csr []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(csr)
	if block == nil {
		return nil, errors.New("failed to parse PEM block")
	}

	return x509.ParseCertificateRequest(block.Bytes)
}
