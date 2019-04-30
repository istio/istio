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
)

const (
	maxCAClientSuccessReturns = 8
)

// FakeCAClient is a fake for testing, implements Client interface.
type FakeCAClient struct {
	certChain []string
	counter   int
}

// NewMockCAClient returns a FakeCAClient.
func NewMockCAClient(certChain []string) *FakeCAClient {
	return &FakeCAClient{
		certChain: certChain,
	}
}

// CSRSign is the fake implementation of CSRSign. It returns the provided certChain until this function
// has reached maxCAClientSuccessReturns.
func (f *FakeCAClient) CSRSign(ctx context.Context, csrPEM []byte, token string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {
	f.counter++
	if f.counter > maxCAClientSuccessReturns {
		return nil, fmt.Errorf("maximum CA success returns has passed")
	}
	return f.certChain, nil
}

// InvokeTimes returns the times that SendCSR has been invoked.
func (f *FakeCAClient) InvokeTimes() int {
	return f.counter
}
