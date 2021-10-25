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

package stsclient

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/security/pkg/monitoring"
	"istio.io/istio/security/pkg/nodeagent/util"
	"istio.io/istio/security/pkg/stsservice/tokenmanager/google/mock"
)

func TestGetFederatedToken(t *testing.T) {
	GKEClusterURL = mock.FakeGKEClusterURL
	r, err := NewSecureTokenServiceExchanger(nil, mock.FakeTrustDomain)
	if err != nil {
		t.Fatalf("failed to create a SecureTokenServiceExchanger: %v", err)
	}
	r.backoff = time.Millisecond

	ms, err := mock.StartNewServer(t, mock.Config{Port: 0})
	if err != nil {
		t.Fatalf("failed to start a mock server: %v", err)
	}
	SecureTokenEndpoint = ms.URL + "/v1/token"
	t.Cleanup(func() {
		if err := ms.Stop(); err != nil {
			t.Logf("failed to stop mock server: %v", err)
		}
		SecureTokenEndpoint = "https://sts.googleapis.com/v1/token"
	})

	t.Run("exchange", func(t *testing.T) {
		token, err := r.ExchangeToken(mock.FakeSubjectToken)
		if err != nil {
			t.Fatalf("failed to call exchange token %v", err)
		}
		if token != mock.FakeFederatedToken {
			t.Errorf("Access token got %q, expected %q", token, mock.FakeFederatedToken)
		}
	})
	t.Run("error", func(t *testing.T) {
		ms.SetGenFedTokenError(errors.New("fake error"))
		t.Cleanup(func() {
			ms.SetGenFedTokenError(nil)
		})
		_, err := r.ExchangeToken(mock.FakeSubjectToken)
		if err == nil {
			t.Fatalf("expected error %v", err)
		}
	})

	t.Run("retry", func(t *testing.T) {
		monitoring.Reset()
		ms.SetGenFedTokenError(errors.New("fake error"))
		_, err := r.ExchangeToken(mock.FakeSubjectToken)
		if err == nil {
			t.Fatalf("expected error %v", err)
		}

		retry.UntilSuccessOrFail(t, func() error {
			g, err := util.GetMetricsCounterValueWithTags("num_outgoing_retries", map[string]string{
				"request_type": monitoring.TokenExchange,
			})
			if err != nil {
				return err
			}
			if g <= 0 {
				return fmt.Errorf("expected retries, got %v", g)
			}
			return nil
		}, retry.Timeout(time.Second*5))
	})
}
