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

package mock

import (
	"context"
	"encoding/pem"
	"fmt"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/pki/util"
)

var (
	sampleKeyCertsPath = "../../../../samples/certs/"
	caCertPath         = path.Join(sampleKeyCertsPath, "ca-cert.pem")
	caKeyPath          = path.Join(sampleKeyCertsPath, "ca-key.pem")
	certChainPath      = path.Join(sampleKeyCertsPath, "cert-chain.pem")
	rootCertPath       = path.Join(sampleKeyCertsPath, "root-cert.pem")
)

// CAClient is the mocked CAClient for testing.
type CAClient struct {
	SignInvokeCount uint64
	errorCount      uint64
	errorCountMutex *sync.Mutex
	errors          uint64
	bundle          util.KeyCertBundle
	certLifetime    time.Duration
	GeneratedCerts  [][]string // Cache the generated certificates for verification purpose.
}

// NewMockCAClient creates an instance of CAClient. errors is used to specify the number of errors
// before CSRSign returns a valid response. certLifetime specifies the TTL for the newly issued workload cert.
func NewMockCAClient(errors uint64, certLifetime time.Duration) (*CAClient, error) {
	cl := CAClient{
		SignInvokeCount: 0,
		errorCount:      0,
		errorCountMutex: &sync.Mutex{},
		errors:          errors,
		certLifetime:    certLifetime,
	}
	bundle, err := util.NewVerifiedKeyCertBundleFromFile(caCertPath, caKeyPath, certChainPath, rootCertPath)
	if err != nil {
		return nil, fmt.Errorf("mock ca client creation error: %v", err)
	}
	cl.bundle = bundle

	atomic.StoreUint64(&cl.SignInvokeCount, 0)
	return &cl, nil
}

// CSRSign returns the certificate or errors depending on the settings.
func (c *CAClient) CSRSign(ctx context.Context, reqID string, csrPEM []byte, exchangedToken string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {
	c.errorCountMutex.Lock()
	if c.errorCount < c.errors {
		c.errorCount++
		c.errorCountMutex.Unlock()
		return nil, status.Error(codes.Unavailable, "CA is unavailable")
	}
	c.errorCountMutex.Unlock()

	atomic.AddUint64(&c.SignInvokeCount, 1)
	signingCert, signingKey, certChain, rootCert := c.bundle.GetAll()
	csr, err := util.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		return nil, fmt.Errorf("csr sign error: %v", err)
	}
	subjectIDs := []string{"test"}
	certBytes, err := util.GenCertFromCSR(csr, signingCert, csr.PublicKey, *signingKey, subjectIDs, c.certLifetime, false)
	if err != nil {
		return nil, fmt.Errorf("csr sign error: %v", err)
	}

	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}
	cert := pem.EncodeToMemory(block)

	ret := []string{string(cert), string(certChain), string(rootCert)}
	c.GeneratedCerts = append(c.GeneratedCerts, ret)
	return ret, nil
}

// TokenExchangeServer is the mocked token exchange server for testing.
type TokenExchangeServer struct {
	errorCount      uint64
	errorCountMutex *sync.Mutex
	errors          uint64
}

// NewMockTokenExchangeServer creates an instance of TokenExchangeServer. errors is used to
// specify the number of errors before ExchangeToken returns a dumb token.
func NewMockTokenExchangeServer(errors uint64) *TokenExchangeServer {
	return &TokenExchangeServer{
		errorCount:      0,
		errorCountMutex: &sync.Mutex{},
		errors:          errors,
	}
}

// ExchangeToken returns a dumb token or errors depending on the settings.
func (s *TokenExchangeServer) ExchangeToken(context.Context, security.CredFetcher, string, string) (string, time.Time, int, error) {
	s.errorCountMutex.Lock()
	if s.errorCount < s.errors {
		s.errorCount++
		s.errorCountMutex.Unlock()
		return "", time.Time{}, 503, fmt.Errorf("service unavailable")
	}
	s.errorCountMutex.Unlock()
	// Since the secret cache uses the k8s token in the stored secret, we can just return anything here.
	return "some-token", time.Now(), 200, nil
}
