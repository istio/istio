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
	"math/big"
)

// CertificateAuthorityInfo includes extra information required for CA
type CertificateAuthorityInfo struct {
	// SerialNumber that has been used so far
	// Recorded to ensure all serial numbers issued by the CA are different
	SerialNumber *big.Int
}

// NewCertificateAuthorityInfo creates a new CertifaceAuthorityInfo with the given serial number
func NewCertificateAuthorityInfo(serialNumber int64) *CertificateAuthorityInfo {
	return &CertificateAuthorityInfo{big.NewInt(serialNumber)}
}

// NewCertificateAuthorityInfoFromJSON creates a new CertifaceAuthorityInfo with the given JSON information
func NewCertificateAuthorityInfoFromJSON(data []byte) (*CertificateAuthorityInfo, error) {
	i := big.NewInt(0)

	if err := i.UnmarshalJSON(data); err != nil {
		return nil, err
	}

	return &CertificateAuthorityInfo{i}, nil
}

// IncSerialNumber increments the given CA Info's serial number
func (n *CertificateAuthorityInfo) IncSerialNumber() {
	n.SerialNumber.Add(n.SerialNumber, big.NewInt(1))
}

// Export transfers the serial number to a JSON format
func (n *CertificateAuthorityInfo) Export() ([]byte, error) {
	return n.SerialNumber.MarshalJSON()
}
