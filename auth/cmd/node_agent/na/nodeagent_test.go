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

	"github.com/golang/glog"
	"google.golang.org/grpc"
	mockutil "istio.io/auth/pkg/util/mock"
	"istio.io/auth/pkg/workload"
	pb "istio.io/auth/proto"
)

const (
	maxCAClientSuccessReturns = 8
)

type FakePlatformSpecificRequest struct {
	dialOption       []grpc.DialOption
	identity         string
	isProperPlatform bool
}

func (f FakePlatformSpecificRequest) GetDialOptions(*Config) ([]grpc.DialOption, error) {
	return f.dialOption, nil
}

func (f FakePlatformSpecificRequest) GetServiceIdentity() (string, error) {
	return f.identity, nil
}

func (f FakePlatformSpecificRequest) IsProperPlatform() bool {
	return f.isProperPlatform
}

type FakeCAClient struct {
	Counter  int
	response *pb.Response
	err      error
}

func (f *FakeCAClient) SendCSR(req *pb.Request, pr platformSpecificRequest, cfg *Config) (*pb.Response, error) {
	f.Counter++
	if f.Counter > maxCAClientSuccessReturns {
		return nil, fmt.Errorf("Terminating the test with errors")
	}
	return f.response, f.err
}

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
	generalConfig := Config{
		"ca_addr", "ca_file", "pkey", "cert_file", "Google Inc.", 512, "onprem", time.Millisecond, 3, 50,
	}
	testCases := map[string]struct {
		config      *Config
		req         platformSpecificRequest
		cAClient    *FakeCAClient
		certUtil    FakeCertUtil
		expectedErr string
		sendTimes   int
		fileContent []byte
	}{
		"Success": {
			config:      &generalConfig,
			req:         FakePlatformSpecificRequest{nil, "service1", true},
			cAClient:    &FakeCAClient{0, &pb.Response{IsApproved: true, SignedCertChain: []byte(`TESTCERT`)}, nil},
			certUtil:    FakeCertUtil{time.Duration(0), nil},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   12,
			fileContent: []byte(`TESTCERT`),
		},
		"Config Nil error": {
			req:         FakePlatformSpecificRequest{nil, "service1", true},
			cAClient:    &FakeCAClient{0, nil, nil},
			expectedErr: "node Agent configuration is nil",
			sendTimes:   0,
		},
		"Platform error": {
			config:      &generalConfig,
			req:         FakePlatformSpecificRequest{nil, "service1", false},
			cAClient:    &FakeCAClient{0, nil, nil},
			expectedErr: "node Agent is not running on the right platform",
			sendTimes:   0,
		},
		"CreateCSR error": {
			// 128 is too small for a RSA private key. GenCSR will return error.
			config: &Config{
				"ca_addr", "ca_file", "pkey", "cert_file", "Google Inc.", 128, "onprem", time.Millisecond, 3, 50,
			},
			req:         FakePlatformSpecificRequest{nil, "service1", true},
			cAClient:    &FakeCAClient{0, nil, nil},
			expectedErr: "failed to generate CSR: crypto/rsa: message too long for RSA public key size",
			sendTimes:   0,
		},
		"SendCSR empty response error": {
			config:      &generalConfig,
			req:         FakePlatformSpecificRequest{nil, "service1", true},
			cAClient:    &FakeCAClient{0, nil, nil},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
		"SendCSR returns error": {
			config:      &generalConfig,
			req:         FakePlatformSpecificRequest{nil, "service1", true},
			cAClient:    &FakeCAClient{0, nil, fmt.Errorf("Error returned from CA")},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
		"SendCSR not approved": {
			config:      &generalConfig,
			req:         FakePlatformSpecificRequest{nil, "service1", true},
			cAClient:    &FakeCAClient{0, &pb.Response{IsApproved: false}, nil},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
		"SendCSR parsing error": {
			config:      &generalConfig,
			req:         FakePlatformSpecificRequest{nil, "service1", true},
			cAClient:    &FakeCAClient{0, &pb.Response{IsApproved: true, SignedCertChain: []byte(`TESTCERT`)}, nil},
			certUtil:    FakeCertUtil{time.Duration(0), fmt.Errorf("cert parsing error")},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
	}

	for id, c := range testCases {
		glog.Errorf("Start to test %s", id)
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
		na := nodeAgentInternal{c.config, c.req, c.cAClient, "service1", fakeWorkloadIO, c.certUtil}
		err := na.Start()
		if err.Error() != c.expectedErr {
			t.Errorf("Test case [%s]: incorrect error message: %s VS %s", id, err.Error(), c.expectedErr)
		}
		if c.cAClient.Counter != c.sendTimes {
			t.Errorf("Test case [%s]: sendCSR is called incorrect times: %d. It should be %d.",
				id, c.cAClient.Counter, c.sendTimes)
		}
		if c.fileContent != nil && !bytes.Equal(fakeFileUtil.WriteContent["cert_file"], c.fileContent) {
			t.Errorf("Test case [%s]: cert file content incorrect: %s vs. %s.",
				id, fakeFileUtil.WriteContent["cert_file"], c.fileContent)
		}
	}
}
