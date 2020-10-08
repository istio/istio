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

package custom

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	mesh "istio.io/api/mesh/v1alpha1"
	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/security/pkg/pki/signingapi"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

const (
	defaultRequestTimeout = 10 * time.Second
)

var (
	cLog = log.RegisterScope("customca", "Custom CA Integration Log", 0)
)

// CAClient generates keys and certificates for Istio identities.
type CAClient struct {
	signingAPIClient *signingapi.Client
	keyCertBundle    util.KeyCertBundle
}

// IsUsingCustomCAForWorkloadCertificate return true if users configure CustomCA for signing Workload Certificates
// TODO: Support for Istio Agent side
func IsUsingCustomCAForWorkloadCertificate(caOpts *mesh.MeshConfig_CA) bool {
	return caOpts != nil && caOpts.Address != "" && caOpts.IstiodSide
}

// NewCAClient returns a new CAClient instance.
func NewCAClient(caOpts *mesh.MeshConfig_CA, keyCertBundle util.KeyCertBundle) (*CAClient, error) {
	requestTimeoutInSecond := defaultRequestTimeout

	if caOpts.GetRequestTimeout() != nil {
		requestTimeoutInSecond = time.Duration(caOpts.GetRequestTimeout().Seconds) * time.Second
	}

	signingAPIClient := signingapi.New(caOpts.GetAddress(), caOpts.TlsSettings, requestTimeoutInSecond)

	c := &CAClient{
		keyCertBundle:    keyCertBundle,
		signingAPIClient: signingAPIClient,
	}

	err := c.signingAPIClient.Connect(nil)
	if err != nil {
		cLog.Errorf("can not connect to Custom CA Address: %v", err)
		return nil, fmt.Errorf("can not connect to Custom CA Address: %v", err)
	}
	return c, nil
}

func responseTimeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	cLog.Debugf("%s took %s", name, elapsed)
}

// CreateCertificate is similar to Sign but returns the leaf cert and the entire cert chain.
func (c *CAClient) CreateCertificate(ctx context.Context,
	req *pb.IstioCertificateRequest, identities []string) (*pb.IstioCertificateResponse, error) {
	if err := verifyCSRIdentities(identities, req.GetCsr()); err != nil {
		return nil, fmt.Errorf("verify CSR Identities failed: %v", err)
	}
	cLog.Debugf("forwarding Workload's CSR to Custom CA server...")
	resp, err := c.signingAPIClient.CreateCertificate(ctx, req)
	certChain := resp.GetCertChain()
	if err != nil {
		cLog.Errorf("cannot call CreateCertificate from Custom CA: %v", err)
		return nil, fmt.Errorf("cannot call CreateCertificate from Custom CA: %v", err)
	}

	var responseCertChains []string
	certChainWithoutRoot := certChain[:len(certChain)-1]
	for _, cert := range certChainWithoutRoot {
		parsedCert, err := validateAndParseCert(cert)
		if err != nil {
			cLog.Errorf("response certificate from Custom CA is invalid: %v", err)
			return nil, fmt.Errorf("response certificate from Custom CA is invalid: %v", err)
		}
		responseCertChains = append(responseCertChains, parsedCert)
	}

	// Append current roots: Custom CA's root CA and Self signing's root CA
	rootCertBytes := c.keyCertBundle.GetRootCertPem()
	responseCertChains = append(responseCertChains, string(rootCertBytes))

	return &pb.IstioCertificateResponse{
		CertChain: responseCertChains,
	}, nil
}

func validateAndParseCert(cert string) (string, error) {
	defer responseTimeTrack(time.Now(), "validateAndParseCert")
	certBytes, _ := pem.Decode([]byte(cert))

	if certBytes == nil {
		return "", fmt.Errorf("decode cert is failed, invalid certificate: %v", cert)
	}

	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes.Bytes,
	}

	c := pem.EncodeToMemory(block)
	return string(c), nil
}

func verifyCSRIdentities(identities []string, csrPEM string) error {
	if len(identities) == 0 {
		return nil
	}

	csrParsed, _ := pem.Decode([]byte(csrPEM))
	if csrParsed == nil {
		return fmt.Errorf("decode csr failed - empty pem: %v", csrPEM)
	}
	parsedCSR, err := x509.ParseCertificateRequest(csrParsed.Bytes)
	if err != nil {
		return fmt.Errorf("cannot parse CSR: %v", err)
	}

	if len(parsedCSR.Subject.CommonName) != 0 {
		return fmt.Errorf("received unsupported CSR, contains Subject.CommonName: %#v", parsedCSR.Subject.CommonName)
	}

	if len(parsedCSR.DNSNames) != 0 {
		return fmt.Errorf("received unsupported CSR, contains DNSNames: %v", parsedCSR.DNSNames)
	}

	if len(parsedCSR.IPAddresses) != 0 {
		return fmt.Errorf("received unsupported CSR, contains IPAddresses: %v", parsedCSR.IPAddresses)
	}

	if len(parsedCSR.EmailAddresses) != 0 {
		return fmt.Errorf("received unsupported CSR, contains EmailAddresses: %v", parsedCSR.EmailAddresses)
	}

	idsFromCSR, err := util.ExtractIDs(parsedCSR.Extensions)
	if err != nil {
		return fmt.Errorf("cannot extract identities from CSR: %v", err)
	}

	if len(idsFromCSR) == 0 {
		return fmt.Errorf("missing spiffe identities in CSR")
	}

	if len(idsFromCSR) > 1 {
		return fmt.Errorf("received unsupported CSR, contains more than 1 identity: %v", idsFromCSR)
	}

	matched := false
	for _, identity := range identities {
		if identity == idsFromCSR[0] {
			matched = true
		}
	}
	if !matched {
		return fmt.Errorf("csr's host (%s) doesn't match with request identities (%s)", idsFromCSR, identities)
	}
	return nil
}
