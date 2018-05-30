// Copyright 2017 Istio Authors
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
	"istio.io/istio/security/pkg/caclient/protocol"
	"istio.io/istio/security/pkg/platform"
	mockpc "istio.io/istio/security/pkg/platform/mock"
	"istio.io/istio/security/pkg/util"
	mockutil "istio.io/istio/security/pkg/util/mock"
	pb "istio.io/istio/security/proto"
)

func TestStartWithArgs(t *testing.T) {
	generalConfig := Config{
		CAClientConfig: caclient.Config{
			CAAddress:  "ca_addr",
			Org:        "Google Inc.",
			RSAKeySize: 512,
			Env:        "onprem",
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
	signedCert := []byte(`TESTCERT`)
	certChain := []byte(`CERTCHAIN`)
	testCases := map[string]struct {
		config      *Config
		pc          platform.Client
		caProtocol  *protocol.FakeProtocol
		certUtil    util.CertUtil
		expectedErr string
		sendTimes   int
		fileContent []byte
	}{
		"Success": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			caProtocol:  protocol.NewFakeProtocol(&pb.CsrResponse{IsApproved: true, SignedCert: signedCert, CertChain: certChain}, ""),
			certUtil:    mockutil.FakeCertUtil{time.Duration(0), nil},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   12,
			fileContent: append(signedCert, certChain...),
		},
		"Config Nil error": {
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			caProtocol:  protocol.NewFakeProtocol(nil, ""),
			expectedErr: "node Agent configuration is nil",
			sendTimes:   0,
		},
		"Platform error": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", false},
			caProtocol:  protocol.NewFakeProtocol(nil, ""),
			expectedErr: "node Agent is not running on the right platform",
			sendTimes:   0,
		},
		"Create CSR error": {
			// 128 is too small for a RSA private key. GenCSR will return error.

			config: &Config{
				CAClientConfig: caclient.Config{
					CAAddress:  "ca_addr",
					Org:        "Google Inc.",
					RSAKeySize: 128,
					Env:        "onprem",
					CSRInitialRetrialInterval: time.Millisecond,
					CSRMaxRetries:             3,
					CSRGracePeriodPercentage:  50,
					RootCertFile:              "ca_file",
					KeyFile:                   "pkey",
					CertChainFile:             "cert_file",
				},
				LoggingOptions: log.DefaultOptions(),
			},
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			caProtocol:  protocol.NewFakeProtocol(nil, ""),
			expectedErr: "CSR creation failed (crypto/rsa: message too long for RSA public key size)",
			sendTimes:   0,
		},
		"Getting agent credential error": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", nil, "Err1", true},
			caProtocol:  protocol.NewFakeProtocol(nil, ""),
			expectedErr: "request creation fails on getting agent credential (Err1)",
			sendTimes:   0,
		},
		"SendCSR empty response error": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			caProtocol:  protocol.NewFakeProtocol(nil, ""),
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
		"SendCSR returns error": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			caProtocol:  protocol.NewFakeProtocol(nil, "error returned from CA"),
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
		"SendCSR not approved": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			caProtocol:  protocol.NewFakeProtocol(&pb.CsrResponse{IsApproved: false}, ""),
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
		"SendCSR parsing error": {
			config: &generalConfig,
			pc:     mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			caProtocol: protocol.NewFakeProtocol(&pb.CsrResponse{
				IsApproved: true, SignedCert: signedCert, CertChain: []byte{},
			}, ""),
			certUtil:    mockutil.FakeCertUtil{time.Duration(0), fmt.Errorf("cert parsing error")},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
	}

	for id, c := range testCases {
		log.Errorf("Start to test %s", id)
		na := nodeAgentInternal{c.config, c.pc, c.caProtocol, "service1", c.certUtil}
		err := na.Start()
		if err.Error() != c.expectedErr {
			t.Errorf("Test case [%s]: incorrect error message: %s VS (expected) %s", id, err.Error(), c.expectedErr)
		}
		if got := c.caProtocol.InvokeTimes(); got != c.sendTimes {
			t.Errorf("Test case [%s]: sendCSR is called incorrect times: %d VS (expected) %d",
				id, got, c.sendTimes)
		}
		// TODO(incfly): add check to compare fileContent equals to the saved secrets after we can read from SecretServer.
	}
}
