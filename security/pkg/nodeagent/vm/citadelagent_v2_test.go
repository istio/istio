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
	"strings"
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

const (
	invalidJwt = `
eyJhbGciOiJSUzI1NiIsImtpZCI6IjI2ZmM0Y2QyM2QzODdjYmM0OTB`
	validJwt = `
eyJhbGciOiJSUzI1NiIsImtpZCI6IjI2ZmM0Y2QyM2QzODdjYmM0OTBmNjBkYjU0YTk0YTZkZDE2NTM5OTgiLCJ0eXAiOiJKV1Qi
fQ.eyJhdWQiOiJteWF1ZGllbmNlIiwiYXpwIjoiMTAwMTc3OTE5NDY5OTgxNzYzMjUwIiwiZW1haWwiOiIxMDAwNTIzMzkzMzQxL
WNvbXB1dGVAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJlbWFpbF92ZXJpZmllZCI6dHJ1ZSwiZXhwIjoxNTU2MjM5M
DQ1LCJnb29nbGUiOnsiY29tcHV0ZV9lbmdpbmUiOnsiaW5zdGFuY2VfY3JlYXRpb25fdGltZXN0YW1wIjoxNTUzMDk5Mjk1LCJpb
nN0YW5jZV9pZCI6IjM4NjAxMTAyMzg5NDY0MTU4NTkiLCJpbnN0YW5jZV9uYW1lIjoiaW5zdGFuY2UtMWEiLCJwcm9qZWN0X2lkI
joibWVzaC1leHBhbnNpb24tMjM1MDIzIiwicHJvamVjdF9udW1iZXIiOjEwMDA1MjMzOTMzNDEsInpvbmUiOiJ1cy1jZW50cmFsM
S1hIn19LCJpYXQiOjE1NTYyMzU0NDUsImlzcyI6Imh0dHBzOi8vYWNjb3VudHMuZ29vZ2xlLmNvbSIsInN1YiI6IjEwMDE3NzkxO
TQ2OTk4MTc2MzI1MCJ9.Kxr1v_HGWptqx_MHwPKEUQh17D4mdw8qeDxbtQDBXFBJXRveTqht7IBkySADn_sdPwBbStUiNQUgMgiG
h9QKmFtlMt8dBhsZqp75Gyqa3AA0VeNipmHBxxOIwS7-jO2AhEvfqOwYqsA9sHq9i-FwXelPUu8qUVwuFug25VlpqhZ6XB4FOMOD
8rCFEl61LBHRgJNbNkr5JoCjSHHM1joTYxlnuzd-9daUxwH8da_sYz0s9oyooCAXEDiLa8_Vlpp9yPcUDbRVgOURv-t8DyULRP4O
0pE7uLKazAOhFsBdl9QmbVa4thGzdD9sxgOvIP-d6MOO5QSK_1NiWyuakBjaMw
`
	// projectID field from the `validJWT` above.
	projectID = "mesh-expansion-235023"
)

func TestCitadelAgentForVM(t *testing.T) {
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
		os.Remove(generalConfig.CAClientConfig.RootCertFile)
		os.Remove(generalConfig.CAClientConfig.KeyFile)
		os.Remove(generalConfig.CAClientConfig.CertChainFile)
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
			pc:          platformMock.FakeClient{Identity: "mock-service-1", AgentCredential: []byte(validJwt), ProperPlatform: true},
			caProtocol:  providerMock.NewMockCAClient([]string{string(certChain)}),
			certUtil:    utilMock.FakeCertUtil{time.Duration(0), nil},
			expectedErr: fmt.Sprintf("max number of retries %d", 3),
			sendTimes:   4,
		},
		"fail invalid platform": {
			config:      &generalConfig,
			pc:          platformMock.FakeClient{ProperPlatform: false},
			expectedErr: "citadel agent is not running on the right platform",
		},
		"fail nil config": {
			config:      nil,
			expectedErr: "citadel agent configuration is nil",
		},
	}

	for tcName, tc := range testCases {
		citadelAgent := citadelAgent{tc.config, tc.pc, tc.caProtocol, "service1", tc.certUtil}
		err := citadelAgent.Start()
		if err != nil {
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Errorf("test `%s` failed. Expected %s, got %v", tcName, tc.expectedErr, err)
			}
		}
	}
}

func TestGetProjectID(t *testing.T) {
	testCases := map[string]struct {
		jwt         string
		projectID   string
		expectedErr string
	}{
		"valid jwt": {
			jwt:         validJwt,
			projectID:   projectID,
			expectedErr: "",
		},
		"invalid jwt": {
			jwt:         invalidJwt,
			projectID:   "",
			expectedErr: fmt.Sprintf("jwt may be invalid: %v", invalidJwt),
		},
	}

	for tcName, tc := range testCases {
		gotProjectID, err := getProjectID(tc.jwt)
		if err != nil {
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Errorf("test `%s` failed with unexpected error %v", tcName, err)
			}
		}
		if gotProjectID != tc.projectID {
			t.Errorf("test `%s` failed. Expect %s, got %s", tcName, tc.projectID, gotProjectID)
		}
	}
}
