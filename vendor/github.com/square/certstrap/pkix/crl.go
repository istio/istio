/*-
 * Copyright 2016 Square Inc.
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
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"time"
)

const (
	crlPEMBlockType = "X509 CRL"
)

func CreateCertificateRevocationList(key *Key, ca *Certificate, expiry time.Time) (*CertificateRevocationList, error) {
	rawCrt, err := ca.GetRawCertificate()
	if err != nil {
		return nil, err
	}

	crlBytes, err := rawCrt.CreateCRL(rand.Reader, key.Private, []pkix.RevokedCertificate{}, time.Now(), expiry)
	if err != nil {
		return nil, err
	}
	return NewCertificateRevocationListFromDER(crlBytes), nil
}

// CertificateSigningRequest is a wrapper around a x509 CertificateRequest and its DER-formatted bytes
type CertificateRevocationList struct {
	derBytes []byte
}

// NewCertificateRevocationListFromDER inits CertificateRevocationList from DER-format bytes
func NewCertificateRevocationListFromDER(derBytes []byte) *CertificateRevocationList {
	return &CertificateRevocationList{derBytes: derBytes}
}

// NewCertificateRevocationListFromPEM inits CertificateRevocationList from PEM-format bytes
func NewCertificateRevocationListFromPEM(data []byte) (*CertificateRevocationList, error) {
	pemBlock, _ := pem.Decode(data)
	if pemBlock == nil {
		return nil, errors.New("cannot find the next PEM formatted block")
	}
	if pemBlock.Type != crlPEMBlockType || len(pemBlock.Headers) != 0 {
		return nil, errors.New("unmatched type or headers")
	}
	return &CertificateRevocationList{derBytes: pemBlock.Bytes}, nil
}

// Export returns PEM-format bytes
func (c *CertificateRevocationList) Export() ([]byte, error) {
	pemBlock := &pem.Block{
		Type:    crlPEMBlockType,
		Headers: nil,
		Bytes:   c.derBytes,
	}

	buf := new(bytes.Buffer)
	if err := pem.Encode(buf, pemBlock); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
