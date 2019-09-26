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

package cainterface

import (
	"time"

	"istio.io/istio/security/pkg/pki/util"
)

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