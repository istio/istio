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

package ra

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/security/pkg/nodeagent/caclient/providers/vault"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

var pkiRaLog = log.RegisterScope("pkiRaLog", "Citadel RA log", 0)

// BackendCAClient interface defines the clients to talk to the backend CA.
type BackendCAClient interface {
	CSRSign(ctx context.Context, reqID string, csrPEM []byte, jwt string,
		certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error)
	GetRootCertPem() string
}

// IstioRA generates keys and certificates for Istio identities.
type IstioRA struct {
	client BackendCAClient
}

// NewIstioRA returns a new IstioRA instance.
func NewIstioRA(backendCAName string, cmGetter corev1.ConfigMapsGetter) (*IstioRA, error) {
	if backendCAName != "Vault" {
		return nil, fmt.Errorf("currently only Vault is supported as the backend CA. %s is not supported", backendCAName)
	}

	client, err := vault.NewVaultClient(cmGetter)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for backend CA: %v", err)
	}
	ra := &IstioRA{
		client: client,
	}
	return ra, nil
}

// Sign takes a PEM-encoded CSR, subject IDs and lifetime, and returns a signed certificate.
func (ra *IstioRA) Sign(csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, forCA bool) ([]byte, error) {
	if forCA {
		return nil, fmt.Errorf("istio RA does not support issue certificates for CAs yet")
	}
	signedCertStrs, err := ra.client.CSRSign(
		context.Background(), "" /* reqID not used */, csrPEM, "" /* not used */, int64(requestedLifetime.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("failed to sign CSR with the backend CA: %v", err)
	}
	// The first returned certificate is the leave certificate.
	return []byte(signedCertStrs[0]), nil
}

// SignWithCertChain is similar to Sign but returns the leaf cert and the entire cert chain.
func (ra *IstioRA) SignWithCertChain(csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, forCA bool) ([]byte, error) {
	if forCA {
		return nil, fmt.Errorf("istio RA does not support issue certificates for CAs yet")
	}
	signedCertStrs, err := ra.client.CSRSign(
		context.Background(), "" /* reqID not used */, csrPEM, "" /* not used */, int64(requestedLifetime.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("failed to sign CSR with the backend CA: %v", err)
	}
	var signedCertBytes []byte
	for _, cert := range signedCertStrs {
		signedCertBytes = append(signedCertBytes, []byte(cert)...)
	}
	return signedCertBytes, nil
}

// GetCAKeyCertBundle is not available.
func (ra *IstioRA) GetCAKeyCertBundle() util.KeyCertBundle {
	pkiRaLog.Errorf("GetCAKeyCertBundle is not available in RA mode")
	return &util.KeyCertBundleImpl{}
}

// GetRootCertPem returns the backend CA's certificate.
func (ra *IstioRA) GetRootCertPem() []byte {
	return []byte(ra.client.GetRootCertPem())
}
