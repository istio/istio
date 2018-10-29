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

package mock

import (
	"time"

	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/pki/util/mock"
)

// FakeCA is a mock of CertificateAuthority.
type FakeCA struct {
	SignedCert    []byte
	SignErr       *ca.Error
	KeyCertBundle util.KeyCertBundle
}

// Sign returns the SignErr if SignErr is not nil, otherwise, it returns SignedCert.
func (ca *FakeCA) Sign([]byte, []string, time.Duration, bool) ([]byte, error) {
	if ca.SignErr != nil {
		return nil, ca.SignErr
	}
	return ca.SignedCert, nil
}

// GetCAKeyCertBundle returns KeyCertBundle if KeyCertBundle is not nil, otherwise, it returns an empty
// FakeKeyCertBundle.
func (ca *FakeCA) GetCAKeyCertBundle() util.KeyCertBundle {
	if ca.KeyCertBundle == nil {
		return &mock.FakeKeyCertBundle{}
	}
	return ca.KeyCertBundle
}
