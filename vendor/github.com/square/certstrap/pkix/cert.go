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
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

const (
	certificatePEMBlockType = "CERTIFICATE"
)

// Certificate is a wrapper around a x509 Certificate and its DER-formatted bytes
type Certificate struct {
	// derBytes is always set for valid Certificate
	derBytes []byte

	crt *x509.Certificate
}

// NewCertificateFromDER inits Certificate from DER-format bytes
func NewCertificateFromDER(derBytes []byte) *Certificate {
	return &Certificate{derBytes: derBytes}
}

// NewCertificateFromPEM inits Certificate from PEM-format bytes
// data should contain at most one certificate
func NewCertificateFromPEM(data []byte) (c *Certificate, err error) {
	pemBlock, _ := pem.Decode(data)
	if pemBlock == nil {
		err = errors.New("cannot find the next PEM formatted block")
		return
	}
	if pemBlock.Type != certificatePEMBlockType || len(pemBlock.Headers) != 0 {
		err = errors.New("unmatched type or headers")
		return
	}
	c = &Certificate{derBytes: pemBlock.Bytes}
	return
}

// build crt field if needed
func (c *Certificate) buildX509Certificate() error {
	if c.crt != nil {
		return nil
	}

	crts, err := x509.ParseCertificates(c.derBytes)
	if err != nil {
		return err
	}
	if len(crts) != 1 {
		return errors.New("unsupported multiple certificates in a block")
	}
	c.crt = crts[0]
	return nil
}

// GetRawCertificate returns a copy of this certificate as an x509.Certificate
func (c *Certificate) GetRawCertificate() (*x509.Certificate, error) {
	if err := c.buildX509Certificate(); err != nil {
		return nil, err
	}
	return c.crt, nil
}

// GetExpirationDuration gets time duration before expiration
func (c *Certificate) GetExpirationDuration() time.Duration {
	if err := c.buildX509Certificate(); err != nil {
		return time.Unix(0, 0).Sub(time.Now())
	}
	return c.crt.NotAfter.Sub(time.Now())
}

// CheckAuthority checks the authority of certificate against itself.
// It only ensures that certificate is self-explanatory, and
// cannot promise the validity and security.
func (c *Certificate) CheckAuthority() error {
	if err := c.buildX509Certificate(); err != nil {
		return err
	}
	return c.crt.CheckSignatureFrom(c.crt)
}

// VerifyHost verifies the host certificate using host name.
// Only certificate of authority could call this function successfully.
// Current implementation allows one CA and direct hosts only,
// so the organization is always this:
//         CA
//  host1 host2 host3
func (c *Certificate) VerifyHost(hostCert *Certificate, name string) error {
	if err := c.CheckAuthority(); err != nil {
		return err
	}

	roots := x509.NewCertPool()
	roots.AddCert(c.crt)

	verifyOpts := x509.VerifyOptions{
		DNSName: "",
		// no intermediates are allowed for now
		Intermediates: nil,
		Roots:         roots,
		// if zero, the current time is used
		CurrentTime: time.Now(),
		// An empty list means ExtKeyUsageServerAuth.
		KeyUsages: nil,
	}

	rawHostCrt, err := hostCert.GetRawCertificate()
	if err != nil {
		return err
	}

	units := rawHostCrt.Subject.OrganizationalUnit
	if len(units) != 1 || units[0] != name {
		return fmt.Errorf("unmatched hostname between %v and %v", units, name)
	}

	chains, err := rawHostCrt.Verify(verifyOpts)
	if err != nil {
		return err
	}
	if len(chains) != 1 {
		return errors.New("internal error: verify chain number != 1")
	}
	return nil
}

// Export returns PEM-format bytes
func (c *Certificate) Export() ([]byte, error) {
	pemBlock := &pem.Block{
		Type:    certificatePEMBlockType,
		Headers: nil,
		Bytes:   c.derBytes,
	}

	buf := new(bytes.Buffer)
	if err := pem.Encode(buf, pemBlock); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
