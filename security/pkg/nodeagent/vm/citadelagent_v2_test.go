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

package vm

import (
	"fmt"
	"os"
	"testing"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/caclient"
	providerMock "istio.io/istio/security/pkg/nodeagent/caclient/providers/mock"
	"istio.io/istio/security/pkg/platform"
	platformMock "istio.io/istio/security/pkg/platform/mock"
	"istio.io/istio/security/pkg/util"
	utilMock "istio.io/istio/security/pkg/util/mock"
)

func Test(t *testing.T) {
	generalConfig := Config{
		CAClientConfig: caclient.Config{
			CAAddress:                 "ca_addr",
			Org:                       "Google Inc.",
			RSAKeySize:                512,
			Env:                       platform.GcpVM,
			CSRInitialRetrialInterval: time.Millisecond,
			CSRMaxRetries:             3,
			CSRGracePeriodPercentage:  50,
			RootCertFile:              "ca_file",
			KeyFile:                   "pkey",
			CertChainFile:             "cert_file",
		},
		LoggingOptions: log.DefaultOptions(),
	}
	defer func() {
		_ = os.Remove(generalConfig.CAClientConfig.RootCertFile)
		_ = os.Remove(generalConfig.CAClientConfig.KeyFile)
		_ = os.Remove(generalConfig.CAClientConfig.CertChainFile)
	}()
	certChain := []byte(`CERTCHAIN`)
	testCases := map[string]struct {
		config      *Config
		pc          platform.Client
		caProtocol  *providerMock.FakeCAClient
		certUtil    util.CertUtil
		expectedErr string
		sendTimes   int
	}{
		"success then fail": {
			config:      &generalConfig,
			pc:          platformMock.FakeClient{Identity: "mock-service-1", AgentCredential: []byte(platformMock.MockJWT), ProperPlatform: true},
			caProtocol:  providerMock.NewMockCAClient([]string{string(certChain)}),
			certUtil:    utilMock.FakeCertUtil{time.Duration(0), nil},
			expectedErr: fmt.Sprintf(maximumRetiresErr, 3),
			sendTimes:   4,
		},
		"fail invalid platform": {
			config:      &generalConfig,
			pc:          platformMock.FakeClient{ProperPlatform: false},
			expectedErr: wrongPlatformErr,
		},
		"fail nil config": {
			config:      nil,
			expectedErr: nilConfigErr,
		},
	}

	for _, tc := range testCases {
		citadelAgent := citadelAgent{tc.config, tc.pc, tc.caProtocol, "service1", tc.certUtil}
		err := citadelAgent.Start()
		if err != nil {
			if err.Error() != tc.expectedErr {
				t.Errorf("test failed. Expected %s, got %v", tc.expectedErr, err)
			}
		}
	}
}
