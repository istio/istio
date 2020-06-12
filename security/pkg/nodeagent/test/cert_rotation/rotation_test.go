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

package rotatesds

import (
	"fmt"
	"os/exec"
	"testing"
	"time"

	"istio.io/istio/pkg/test/env"
	sdsTest "istio.io/istio/security/pkg/nodeagent/test"
)

const (
	rotateInterval   = 1500 * time.Millisecond
	proxyRunningTime = 6 * rotateInterval
	sleepTime        = 100 * time.Millisecond
	retryAttempt     = 3
)

type void struct{}

var (
	certSet = make(map[string]void)
	member  void
)

func TestCertRotation(t *testing.T) {
	sdsTest.RotateCert(rotateInterval)
	setup := sdsTest.SetupTest(t, env.SDSCertRotation)
	defer setup.TearDown()
	setup.StartProxy(t)
	start := time.Now()
	numReq := 0
	for {
		code, _, err := env.HTTPGet(fmt.Sprintf("http://localhost:%d/echo", setup.OutboundListenerPort))
		if err != nil {
			t.Errorf("Failed in request: %v", err)
		}
		if code != 200 {
			t.Errorf("Unexpected status code: %d", code)
		}
		numReq++
		if time.Since(start) > proxyRunningTime {
			break
		}
		time.Sleep(sleepTime)
		cert, err := GetInboundCert(setup.InboundListenerPort)

		if err != nil {
			continue
		}
		certSet[cert] = member
	}

	stats, err := setup.ProxySetup.GetStatsMap()
	if err == nil {
		numSSLHandshake := stats["cluster.outbound_cluster_tls.ssl.handshake"]
		numSSLConnError := stats[fmt.Sprintf("listener.127.0.0.1_%d.ssl.connection_error", setup.InboundListenerPort)]
		numSSLVerifyNoCert := stats[fmt.Sprintf("listener.127.0.0.1_%d.ssl.fail_verify_no_cert", setup.InboundListenerPort)]
		numSSLVerifyCAError := stats[fmt.Sprintf("listener.127.0.0.1_%d.ssl.fail_verify_error", setup.InboundListenerPort)]
		numOutboundSDSRotate := stats["cluster.outbound_cluster_tls.client_ssl_socket_factory.ssl_context_update_by_sds"]
		numInboundSDSRotate := len(certSet) - 1
		// Cluster config max_requests_per_connection is set to 1, the number of requests should match
		// the number of SSL connections. This guarantees SSL connection is using the latest TLS key/cert loaded in Envoy.
		if numSSLHandshake != uint64(numReq) {
			t.Errorf("Number of successful SSL handshake does not match, expect %d but get %d", numReq, numSSLHandshake)
		}
		if numSSLConnError != 0 {
			t.Errorf("Number of SSL connection error: %d", numSSLConnError)
		}
		if numSSLVerifyNoCert != 0 {
			t.Errorf("Number of SSL handshake failures because of missing client cert: %d", numSSLVerifyNoCert)
		}
		if numSSLVerifyCAError != 0 {
			t.Errorf("Number of SSL handshake failures on CA verification: %d", numSSLVerifyCAError)
		}
		// Verify that there are multiple SDS updates. TLS key/cert are loaded multiple times.
		if numOutboundSDSRotate < 1 {
			t.Errorf("Number of SDS rotate at outbound cluster should be greater than zero, get %d", numOutboundSDSRotate)
		}
		if numInboundSDSRotate < 1 {
			t.Errorf("Number of SDS rotate at inbound listener should be greater than zero, get %d", numInboundSDSRotate)
		}
	} else {
		t.Errorf("cannot get Envoy stats: %v", err)
	}
}

// get Cert from the InboundListener
func GetInboundCert(inboundListenerPort int) (string, error) {
	return openssl("s_client", "-showcerts",
		"-connect", fmt.Sprintf("127.0.0.1:%d", inboundListenerPort),
	)
}

func openssl(args ...string) (string, error) {
	cmd := exec.Command("openssl", args...)
	var err error
	var out []byte
	for attempt := 0; attempt < retryAttempt; attempt++ {
		out, err = cmd.Output()
		if err == nil {
			return string(out), nil
		}
		time.Sleep(2 * sleepTime)
	}
	return string(out), fmt.Errorf("command %s failed: %q %v", cmd.String(), string(out), err)
}
