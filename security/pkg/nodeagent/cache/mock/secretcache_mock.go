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

package mock

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CAClient struct {
	signInvokeCount     uint64
	mockCertChain1st    []string
	mockCertChainRemain []string
	// Number of calls so far
	numOfCalls int
	// Number of calls before the mocked CSRSign() succeeds,
	// e.g., when numOfCallsBeforeSucceed = 1,
	// the second call of CSRSign() succeeds while the first call fails.
	numOfCallsBeforeSucceed int
}

// Create a CA client that sends CSR.
func NewMockCAClient(mockCertChain1st, mockCertChainRemain []string, numOfCallsBeforeSucceed int) *CAClient {
	cl := CAClient{
		mockCertChain1st:        mockCertChain1st,
		mockCertChainRemain:     mockCertChainRemain,
		numOfCalls:              0,
		numOfCallsBeforeSucceed: numOfCallsBeforeSucceed,
	}

	atomic.StoreUint64(&cl.signInvokeCount, 0)
	return &cl
}

func (c *CAClient) CSRSign(ctx context.Context, reqID string, csrPEM []byte, exchangedToken string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {
	c.numOfCalls++
	// Based on numOfCallsBeforeSucceed, mock CSRSign failure errors to force Citadel agent to retry.
	if c.numOfCalls <= c.numOfCallsBeforeSucceed {
		return nil, status.Error(codes.Unavailable, "CA is unavailable")
	}
	// reset the number of calls when CSRSign does not return failure.
	c.numOfCalls = 0

	if atomic.LoadUint64(&c.signInvokeCount) == 0 {
		atomic.AddUint64(&c.signInvokeCount, 1)
		return nil, status.Error(codes.Internal, "some internal error")
	}

	if atomic.LoadUint64(&c.signInvokeCount) == 1 {
		atomic.AddUint64(&c.signInvokeCount, 1)
		return c.mockCertChain1st, nil
	}

	return c.mockCertChainRemain, nil
}

type TokenExchangeServer struct {
}

func NewMockTokenExchangeServer() *TokenExchangeServer {
	return &TokenExchangeServer{}
}

func (s *TokenExchangeServer) ExchangeToken(context.Context, string, string) (string, time.Time, int, error) {
	// Mock ExchangeToken failure errors to force Citadel agent to retry.
	// 50% chance of failure.
	if rand.Intn(2) != 0 {
		return "", time.Time{}, 503, fmt.Errorf("service unavailable")
	}
	// Since the secret cache uses the k8s token in the stored secret, we can just return anything here.
	return "some-token", time.Now(), 200, nil
}
