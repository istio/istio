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

package na

import (
	"bytes"
	"fmt"
	"testing"
	"time"
	// TODO(nmittler): Remove this
	_ "github.com/golang/glog"

	"istio.io/istio/pkg/log"
	mockclient "istio.io/istio/security/pkg/caclient/grpc/mock"
	"istio.io/istio/security/pkg/platform"
	mockpc "istio.io/istio/security/pkg/platform/mock"
	mockutil "istio.io/istio/security/pkg/util/mock"
	"istio.io/istio/security/pkg/workload"
	pb "istio.io/istio/security/proto"
)

type FakeCertUtil struct {
	duration time.Duration
	err      error
}

func (f FakeCertUtil) GetWaitTime(certBytes []byte, now time.Time, gracePeriodPercentage int) (time.Duration, error) {
	if f.err != nil {
		return time.Duration(0), f.err
	}
	return f.duration, nil
}

func TestStartWithArgs(t *testing.T) {
	generalPcConfig := platform.ClientConfig{
		OnPremConfig: platform.OnPremConfig{
			RootCACertFile: "ca_file",
			KeyFile:        "pkey",
			CertChainFile:  "cert_file",
		},
	}
	generalConfig := Config{
		IstioCAAddress:     "ca_addr",
		ServiceIdentityOrg: "Google Inc.",
		RSAKeySize:         512,
		Env:                "onprem",
		CSRInitialRetrialInterval: time.Millisecond,
		CSRMaxRetries:             3,
		CSRGracePeriodPercentage:  50,
		PlatformConfig:            generalPcConfig,
		LoggingOptions:            log.NewOptions(),
	}
	testCases := map[string]struct {
		config      *Config
		pc          platform.Client
		cAClient    *mockclient.FakeCAClient
		certUtil    FakeCertUtil
		expectedErr string
		sendTimes   int
		fileContent []byte
	}{
		"Success": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			cAClient:    &mockclient.FakeCAClient{0, &pb.CsrResponse{IsApproved: true, SignedCertChain: []byte(`TESTCERT`)}, nil},
			certUtil:    FakeCertUtil{time.Duration(0), nil},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   12,
			fileContent: []byte(`TESTCERT`),
		},
		"Config Nil error": {
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			cAClient:    &mockclient.FakeCAClient{0, nil, nil},
			expectedErr: "node Agent configuration is nil",
			sendTimes:   0,
		},
		"Platform error": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", false},
			cAClient:    &mockclient.FakeCAClient{0, nil, nil},
			expectedErr: "node Agent is not running on the right platform",
			sendTimes:   0,
		},
		"Create CSR error": {
			// 128 is too small for a RSA private key. GenCSR will return error.

			config: &Config{
				IstioCAAddress:     "ca_addr",
				ServiceIdentityOrg: "Google Inc.",
				RSAKeySize:         128,
				Env:                "onprem",
				CSRInitialRetrialInterval: time.Millisecond,
				CSRMaxRetries:             3,
				CSRGracePeriodPercentage:  50,
				PlatformConfig:            generalPcConfig,
				LoggingOptions:            log.NewOptions(),
			},
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			cAClient:    &mockclient.FakeCAClient{0, nil, nil},
			expectedErr: "CSR creation failed (crypto/rsa: message too long for RSA public key size)",
			sendTimes:   0,
		},
		"Getting agent credential error": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", nil, "Err1", true},
			cAClient:    &mockclient.FakeCAClient{0, nil, nil},
			expectedErr: "request creation fails on getting agent credential (Err1)",
			sendTimes:   0,
		},
		"SendCSR empty response error": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			cAClient:    &mockclient.FakeCAClient{0, nil, nil},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
		"SendCSR returns error": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			cAClient:    &mockclient.FakeCAClient{0, nil, fmt.Errorf("error returned from CA")},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
		"SendCSR not approved": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			cAClient:    &mockclient.FakeCAClient{0, &pb.CsrResponse{IsApproved: false}, nil},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
		"SendCSR parsing error": {
			config:      &generalConfig,
			pc:          mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			cAClient:    &mockclient.FakeCAClient{0, &pb.CsrResponse{IsApproved: true, SignedCertChain: []byte(`TESTCERT`)}, nil},
			certUtil:    FakeCertUtil{time.Duration(0), fmt.Errorf("cert parsing error")},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
	}

	for id, c := range testCases {
		log.Errorf("Start to test %s", id)
		fakeFileUtil := mockutil.FakeFileUtil{
			ReadContent:  make(map[string][]byte),
			WriteContent: make(map[string][]byte),
		}
		fakeWorkloadIO, _ := workload.NewSecretServer(
			workload.Config{
				Mode:                          workload.SecretFile,
				FileUtil:                      fakeFileUtil,
				ServiceIdentityCertFile:       "cert_file",
				ServiceIdentityPrivateKeyFile: "key_file",
			},
		)
		na := nodeAgentInternal{c.config, c.pc, c.cAClient, "service1", fakeWorkloadIO, c.certUtil}
		err := na.Start()
		if err.Error() != c.expectedErr {
			t.Errorf("Test case [%s]: incorrect error message: %s VS (expected) %s", id, err.Error(), c.expectedErr)
		}
		if c.cAClient.Counter != c.sendTimes {
			t.Errorf("Test case [%s]: sendCSR is called incorrect times: %d VS (expected) %d",
				id, c.cAClient.Counter, c.sendTimes)
		}
		if c.fileContent != nil && !bytes.Equal(fakeFileUtil.WriteContent["cert_file"], c.fileContent) {
			t.Errorf("Test case [%s]: cert file content incorrect: %s VS (expected) %s",
				id, fakeFileUtil.WriteContent["cert_file"], c.fileContent)
		}
	}
}
