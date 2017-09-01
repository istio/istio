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
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc"
	pb "istio.io/auth/proto"
)

const (
	// This cert has:
	//   NotBefore = 2017-08-23 19:00:40 +0000 UTC
	//   NotAfter  = 2017-08-24 19:00:40 +0000 UTC
	testCert = `
-----BEGIN CERTIFICATE-----
MIICjzCCAfigAwIBAgIJAIv7HoEtVOuKMA0GCSqGSIb3DQEBCwUAMIGIMQswCQYD
VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTEWMBQGA1UEBwwNU2FuIEZyYW5j
aXNjbzENMAsGA1UECgwETHlmdDENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHVGVz
dCBDQTEcMBoGCSqGSIb3DQEJARYNdGVzdEBseWZ0LmNvbTAeFw0xNzA4MjMxOTAw
NDBaFw0xNzA4MjQxOTAwNDBaMEExCzAJBgNVBAYTAlVTMQswCQYDVQQIDAJDQTEW
MBQGA1UEBwwNU2FuIEZyYW5jaXNjbzENMAsGA1UECwwETHlmdDCBnzANBgkqhkiG
9w0BAQEFAAOBjQAwgYkCgYEA0DUnVXEAZZiwR6MjLTCa2ETCOB5LgmKrm7wQ/YH2
W7fm7seS4dgkU0VQfX1E4f20PX82fl6Wbtjixv6LR4ttGLuVz4Fy+fjkswnczKro
xrPBbfboGSNlC07n6jH3rG7O8vHCYNsvlKUGZV5SdoiJjJKnaJjrKzb8xc09/y0Z
3MsCAwEAAaNHMEUwCQYDVR0TBAIwADALBgNVHQ8EBAMCBeAwKwYDVR0RBCQwIoYg
aXN0aW86YWNjb3VudDEuZm9vLmNsdXN0ZXIubG9jYWwwDQYJKoZIhvcNAQELBQAD
gYEAjxzEoNd8U7BERKPYsQiRjM8XeweLf/smOFvUz+JKeBzUKkKuQ4GrrPl0bpfn
1Fh7XNGXcsGJjN3Q15C9e5f9hKLY0jjNRQaGftcT8gb2wMMvps8UrBe/VKRZUt+i
TUwOfQd26zZxdjg/Cl6pCT6yDQlUH7Pzf9pogM7gHjd9sFA=
-----END CERTIFICATE-----`
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
	return f.response, f.err
}

func TestStartWithArgs(t *testing.T) {
	generalConfig := Config{
		"ca_addr", "ca_file", "pkey", "cert_file", "Google Inc.", 512, "onprem", time.Millisecond, 3, 50,
	}
	testCases := map[string]struct {
		config      *Config
		req         platformSpecificRequest
		cAClient    *FakeCAClient
		expectedErr string
		sendTimes   int
	}{
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
			cAClient:    &FakeCAClient{0, &pb.Response{IsApproved: true, SignedCertChain: []byte(`INVALIDCERT`)}, nil},
			expectedErr: "node agent can't get the CSR approved from Istio CA after max number of retries (3)",
			sendTimes:   4,
		},
	}

	for id, c := range testCases {
		na := nodeAgentInternal{c.config, c.req, c.cAClient, "service1"}
		err := na.Start()
		if err.Error() != c.expectedErr {
			t.Errorf("%s: incorrect error message: %s VS %s", id, err.Error(), c.expectedErr)
		}
		if c.cAClient.Counter != c.sendTimes {
			t.Errorf("SendCSR is called incorrect times: %d. It should be %d.", c.cAClient.Counter, c.sendTimes)
		}
	}
}

func TestGettingWaitTimeFromCert(t *testing.T) {
	testCases := map[string]struct {
		cert             string
		now              time.Time
		expectedWaitTime int
		expectedErr      string
	}{
		"Success": {
			// Now = 2017-08-23 21:00:40 +0000 UTC
			// Cert TTL is 24h, and grace period is 50% of TTL, that is 12h.
			// The cert expires at 2017-08-24 19:00:40 +0000 UTC, so the grace period starts at 2017-08-24 07:00:40 +0000 UTC
			// The wait time is the duration from fake now to the grace period start time, which is 10h39s (36039s).
			cert:             testCert,
			now:              time.Date(2017, time.August, 23, 21, 00, 00, 40, time.UTC),
			expectedWaitTime: 36039,
		},
		"Cert expired": {
			// Now = 2017-08-25 21:00:40 +0000 UTC.
			// Now is later than cert's NotAfter 2017-08-24 19:00:40 +0000 UTC.
			cert: testCert,
			now:  time.Date(2017, time.August, 25, 21, 00, 00, 40, time.UTC),
			expectedErr: "certificate already expired at 2017-08-24 19:00:40 +0000" +
				" UTC, but now is 2017-08-25 21:00:00.00000004 +0000 UTC",
		},
		"Renew now": {
			// Now = 2017-08-24 16:00:40 +0000 UTC
			// Now is later than the start of grace period 2017-08-24 07:00:40 +0000 UTC, but earlier than
			// cert expiration 2017-08-24 19:00:40 +0000 UTC.
			cert:        testCert,
			now:         time.Date(2017, time.August, 24, 16, 00, 00, 40, time.UTC),
			expectedErr: "got a certificate that should be renewed now",
		},
		"Invalid cert pem": {
			cert:        `INVALIDCERT`,
			now:         time.Date(2017, time.August, 23, 21, 00, 00, 40, time.UTC),
			expectedErr: "Invalid PEM encoded certificate",
		},
	}

	for id, c := range testCases {
		config := Config{
			"ca_addr", "ca_file", "pkey", "cert_file", "Google Inc.", 512, "onprem", time.Millisecond, 3, 50,
		}
		req := FakePlatformSpecificRequest{nil, "service1", true}
		caClient := FakeCAClient{0, nil, nil}
		na := nodeAgentInternal{&config, req, &caClient, "service1"}
		waitTime, err := na.getWaitTimeFromCert([]byte(c.cert), c.now, 50)
		if c.expectedErr != "" {
			if err == nil {
				t.Errorf("%s: no error is returned.", id)
			}
			if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s", id, err.Error(), c.expectedErr)
			}
		} else {
			if err != nil {
				t.Errorf("%s: unexpected error: %v", id, err)
			}
			if int(waitTime.Seconds()) != c.expectedWaitTime {
				t.Errorf("%s: incorrect waittime. Expected %ds, but got %ds.", id, c.expectedWaitTime, int(waitTime.Seconds()))
			}
		}
	}
}
