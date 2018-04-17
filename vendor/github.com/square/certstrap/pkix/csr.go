/*-
 * Copyright 2015 Square Inc.
 * Copyright 2014 CoreOS
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package pkix

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
)

const (
	csrPEMBlockType = "CERTIFICATE REQUEST"
)

var (
	csrPkixName = pkix.Name{
		Country:            []string{},
		Organization:       []string{},
		OrganizationalUnit: nil,
		Locality:           nil,
		Province:           nil,
		StreetAddress:      nil,
		PostalCode:         nil,
		SerialNumber:       "",
		CommonName:         "",
	}
)

// ParseAndValidateIPs parses a comma-delimited list of IP addresses into an array of IP addresses
func ParseAndValidateIPs(ipList string) (res []net.IP, err error) {
	// IP list can potentially be a blank string, ""
	if len(ipList) > 0 {
		ips := strings.Split(ipList, ",")
		for _, ip := range ips {
			parsedIP := net.ParseIP(ip)
			if parsedIP == nil {
				return nil, fmt.Errorf("Invalid IP address: %s", ip)
			}
			res = append(res, parsedIP)
		}
	}
	return
}

// CreateCertificateSigningRequest sets up a request to create a csr file with the given parameters
func CreateCertificateSigningRequest(key *Key, organizationalUnit string, ipList []net.IP, domainList []string, organization string, country string, province string, locality string, commonName string) (*CertificateSigningRequest, error) {

	csrPkixName.CommonName = commonName

	if len(organizationalUnit) > 0 {
		csrPkixName.OrganizationalUnit = []string{organizationalUnit}
	}
	if len(organization) > 0 {
		csrPkixName.Organization = []string{organization}
	}
	if len(country) > 0 {
		csrPkixName.Country = []string{country}
	}
	if len(province) > 0 {
		csrPkixName.Province = []string{province}
	}
	if len(locality) > 0 {
		csrPkixName.Locality = []string{locality}
	}
	csrTemplate := &x509.CertificateRequest{
		Subject:     csrPkixName,
		IPAddresses: ipList,
		DNSNames:    domainList,
	}

	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key.Private)
	if err != nil {
		return nil, err
	}
	return NewCertificateSigningRequestFromDER(csrBytes), nil
}

// CertificateSigningRequest is a wrapper around a x509 CertificateRequest and its DER-formatted bytes
type CertificateSigningRequest struct {
	// derBytes is always set for valid Certificate
	derBytes []byte

	cr *x509.CertificateRequest
}

// NewCertificateSigningRequestFromDER inits CertificateSigningRequest from DER-format bytes
func NewCertificateSigningRequestFromDER(derBytes []byte) *CertificateSigningRequest {
	return &CertificateSigningRequest{derBytes: derBytes}
}

// NewCertificateSigningRequestFromPEM inits CertificateSigningRequest from PEM-format bytes
// data should contain at most one certificate
func NewCertificateSigningRequestFromPEM(data []byte) (*CertificateSigningRequest, error) {
	pemBlock, _ := pem.Decode(data)
	if pemBlock == nil {
		return nil, errors.New("cannot find the next PEM formatted block")
	}
	if pemBlock.Type != csrPEMBlockType || len(pemBlock.Headers) != 0 {
		return nil, errors.New("unmatched type or headers")
	}
	return &CertificateSigningRequest{derBytes: pemBlock.Bytes}, nil
}

// build cr field if needed
func (c *CertificateSigningRequest) buildPKCS10CertificateSigningRequest() error {
	if c.cr != nil {
		return nil
	}

	var err error
	c.cr, err = x509.ParseCertificateRequest(c.derBytes)
	if err != nil {
		return err
	}
	return nil
}

// GetRawCertificateSigningRequest returns a copy of this certificate request as an x509.CertificateRequest.
func (c *CertificateSigningRequest) GetRawCertificateSigningRequest() (*x509.CertificateRequest, error) {
	if err := c.buildPKCS10CertificateSigningRequest(); err != nil {
		return nil, err
	}
	return c.cr, nil
}

// CheckSignature verifies that the signature is a valid signature
// using the public key in CertificateSigningRequest.
func (c *CertificateSigningRequest) CheckSignature() error {
	if err := c.buildPKCS10CertificateSigningRequest(); err != nil {
		return err
	}
	return checkSignature(c.cr, c.cr.SignatureAlgorithm, c.cr.RawTBSCertificateRequest, c.cr.Signature)
}

// checkSignature verifies a signature made by the key on a CSR, such
// as on the CSR itself.
func checkSignature(csr *x509.CertificateRequest, algo x509.SignatureAlgorithm, signed, signature []byte) error {
	var hashType crypto.Hash
	switch algo {
	case x509.SHA1WithRSA, x509.ECDSAWithSHA1:
		hashType = crypto.SHA1
	case x509.SHA256WithRSA, x509.ECDSAWithSHA256:
		hashType = crypto.SHA256
	case x509.SHA384WithRSA, x509.ECDSAWithSHA384:
		hashType = crypto.SHA384
	case x509.SHA512WithRSA, x509.ECDSAWithSHA512:
		hashType = crypto.SHA512
	default:
		return x509.ErrUnsupportedAlgorithm
	}
	if !hashType.Available() {
		return x509.ErrUnsupportedAlgorithm
	}
	h := hashType.New()
	h.Write(signed)
	digest := h.Sum(nil)
	switch pub := csr.PublicKey.(type) {
	case *rsa.PublicKey:
		return rsa.VerifyPKCS1v15(pub, hashType, digest, signature)
	case *ecdsa.PublicKey:
		ecdsaSig := new(struct{ R, S *big.Int })
		if _, err := asn1.Unmarshal(signature, ecdsaSig); err != nil {
			return err
		}
		if ecdsaSig.R.Sign() <= 0 || ecdsaSig.S.Sign() <= 0 {
			return errors.New("x509: ECDSA signature contained zero or negative values")
		}
		if !ecdsa.Verify(pub, digest, ecdsaSig.R, ecdsaSig.S) {
			return errors.New("x509: ECDSA verification failure")
		}
		return nil
	}
	return x509.ErrUnsupportedAlgorithm
}

// Export returns PEM-format bytes
func (c *CertificateSigningRequest) Export() ([]byte, error) {
	pemBlock := &pem.Block{
		Type:    csrPEMBlockType,
		Headers: nil,
		Bytes:   c.derBytes,
	}

	buf := new(bytes.Buffer)
	if err := pem.Encode(buf, pemBlock); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
