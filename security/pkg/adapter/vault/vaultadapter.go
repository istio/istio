// Copyright 2018 Istio Authors
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

package adapter

import (
	"fmt"
	"time"
)

// VaultAdapter connects to Vault to sign certificates.
type VaultAdapter struct {
}

// NewVaultAdapter returns a new NewVaultAdapter instance.
func NewVaultAdapter() (*VaultAdapter, error) {
	return &VaultAdapter{}, nil
}

// Sign takes a PEM-encoded CSR and returns a signed certificate. If the CA is a multicluster CA,
// the signed certificate is a CA certificate, otherwise, it is a workload certificate.
func (v *VaultAdapter) Sign(csrPEM []byte, ttl time.Duration) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

// SignCAServerCert signs the certificate for the Istio CA server.
func (v *VaultAdapter) SignCAServerCert(csrPEM []byte, ttl time.Duration) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetCertChain returns the certificate chain for the Vault CA.
func (v *VaultAdapter) GetCertChain() []byte {
	return nil
}

// GetRootCert returns the root certificate for the Vault CA.
func (v *VaultAdapter) GetRootCert() []byte {
	return nil
}
