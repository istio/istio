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

package test

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	ghc "google.golang.org/grpc/health/grpc_health_v1"

	"istio.io/istio/pkg/security"

	"istio.io/istio/pkg/spiffe"
	istioEnv "istio.io/istio/pkg/test/env"
	"istio.io/istio/security/pkg/nodeagent/cache"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
	caserver "istio.io/istio/security/pkg/nodeagent/test/mock"
)

const (
	proxyTokenPath = "/tmp/sds-envoy-token.jwt"
	jwtToken       = "eyJhbGciOiJSUzI1NiIsImtpZCI6IkRIRmJwb0lVcXJZOHQyenBBMnFYZkNtcjVWTzVaRXI0UnpIVV8tZW52dlEiLCJ0eXAiOiJKV1QifQ." +
		"eyJleHAiOjQ2ODU5ODk3MDAsImZvbyI6ImJhciIsImlhdCI6MTUzMjM4OTcwMCwiaXNzIjoidGVzdGluZ0BzZWN1cmUuaXN0aW8uaW8iLCJzdWIiOiJ0ZX" +
		"N0aW5nQHNlY3VyZS5pc3Rpby5pbyJ9.CfNnxWP2tcnR9q0vxyxweaF3ovQYHYZl82hAUsn21bwQd9zP7c-LS9qd_vpdLG4Tn1A15NxfCjp5f7QNBUo-KC9" +
		"PJqYpgGbaXhaGx7bEdFWjcwv3nZzvc7M__ZpaCERdwU7igUmJqYGBYQ51vr2njU9ZimyKkfDe3axcyiBZde7G6dabliUosJvvKOPcKIWPccCgefSj_GNfw" +
		"Iip3-SsFdlR7BtbVUcqR-yv-XOxJ3Uc1MI0tz3uMiiZcyPV7sNCU4KRnemRIMHVOfuvHsU60_GhGbiSFzgPTAa9WTltbnarTbxudb_YEOx12JiwYToeX0D" +
		"CPb43W1tzIBxgm8NxUg"
)

var rotateCertInterval time.Duration

// RotateCert forces cert to rotate at a specified interval for testing.
// Setting this to 0 disables rotation.
func RotateCert(interval time.Duration) {
	rotateCertInterval = interval
}

// Env manages test setup and teardown.
type Env struct {
	ProxySetup           *istioEnv.TestSetup
	OutboundListenerPort int
	InboundListenerPort  int
	// SDS server
	SDSServer *sds.Server
	// CA server
	CAServer *caserver.CAServer
}

// TearDown tears down all components.
func (e *Env) TearDown() {
	// Stop proxy first, otherwise XDS stream is still alive and server's graceful
	// stop will be blocked.
	e.ProxySetup.TearDown()
	e.SDSServer.Stop()
	e.CAServer.GRPCServer.GracefulStop()
}

func getDataFromFile(filePath string, t *testing.T) []byte {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %q", filePath)
	}
	return data
}

// WriteDataToFile writes data into file
func WriteDataToFile(path string, content []byte) error {
	if path == "" {
		return errors.New("empty file path")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(content); err != nil {
		return err
	}
	_ = f.Sync()
	return nil
}

// SetupTest starts Envoy, SDS server, CA server and a dummy backend.
// The test allow HTTP request flow.
//                                 CA server
//                                     |
//                        +--------SDS server--------+
//                        |                          |
// HTTP    ->outbound ->outbound TLS <--mTLS-->inbound TLS ->inbound ->backend
// request   listener   cluster                listener      cluster
func SetupTest(t *testing.T, testID uint16) *Env {
	// Set up credential files for bootstrap config
	if err := WriteDataToFile(proxyTokenPath, []byte(jwtToken)); err != nil {
		t.Fatalf("failed to set up token file %s: %v", proxyTokenPath, err)
	}

	env := &Env{}
	// Set up test environment for Proxy
	proxySetup := istioEnv.NewTestSetup(testID, t)
	proxySetup.EnvoyTemplate = string(getDataFromFile(istioEnv.IstioSrc+"/security/pkg/nodeagent/test/testdata/bootstrap.yaml", t))
	env.ProxySetup = proxySetup
	env.OutboundListenerPort = int(proxySetup.Ports().ClientProxyPort)
	env.InboundListenerPort = int(proxySetup.Ports().ServerProxyPort)
	env.DumpPortMap(t)
	ca, err := caserver.NewCAServer(int(proxySetup.Ports().ExtraPort))
	if err != nil {
		t.Fatalf("failed to start CA server: %+v", err)
	}
	env.CAServer = ca
	env.waitForCAReady(t)
	env.StartSDSServer(t)
	return env
}

// DumpPortMap dumps port allocation status
// outbound listener      : ClientProxyPort
// inbound listener       : ServerProxyPort
// test backend           : BackendPort
// proxy admin            : AdminPort
// SDS path               : SDSPath
func (e *Env) DumpPortMap(t *testing.T) {
	t.Logf("\n\tport allocation status\t\t\t\n"+
		"proxy admin\t\t\t:\t%d\n"+
		"outbound listener\t\t:\t%d\n"+
		"inbound listener\t\t:\t%d\n"+
		"test backend\t\t\t:\t%d\n"+
		"CSR server\t\t\t:\t%d\n"+
		"SDS path\t\t\t:\t%s\n", e.ProxySetup.Ports().AdminPort,
		e.ProxySetup.Ports().ClientProxyPort,
		e.ProxySetup.Ports().ServerProxyPort, e.ProxySetup.Ports().BackendPort,
		e.ProxySetup.Ports().ExtraPort, e.ProxySetup.SDSPath())
}

// StartProxy starts proxy.
func (e *Env) StartProxy(t *testing.T) {
	if err := e.ProxySetup.SetUp(); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	t.Log("proxy is running...")
}

// StartSDSServer starts SDS server
func (e *Env) StartSDSServer(t *testing.T) {
	serverOptions := &security.Options{
		WorkloadUDSPath:   e.ProxySetup.SDSPath(),
		UseLocalJWT:       true,
		JWTPath:           proxyTokenPath,
		CAEndpoint:        fmt.Sprintf("127.0.0.1:%d", e.ProxySetup.Ports().ExtraPort),
		EnableWorkloadSDS: true,
		RecycleInterval:   5 * time.Minute,
	}

	caClient, err := citadel.NewCitadelClient(serverOptions.CAEndpoint, false, nil, "")
	if err != nil {
		t.Fatalf("failed to create CA client: %+v", err)
	}
	secretFetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    caClient,
	}
	opt := e.cacheOptions(t)
	workloadSecretCache := cache.NewSecretCache(secretFetcher, sds.NotifyProxy, opt)
	sdsServer, err := sds.NewServer(serverOptions, workloadSecretCache, nil)
	if err != nil {
		t.Fatalf("failed to start SDS server: %+v", err)
	}
	e.SDSServer = sdsServer
}

func (e *Env) cacheOptions(t *testing.T) *security.Options {
	// Default options does not rotate cert until cert expires after 1 hour.
	opt := &security.Options{
		SecretTTL:                      1 * time.Hour,
		TrustDomain:                    spiffe.GetTrustDomain(),
		RotationInterval:               5 * time.Minute,
		SecretRotationGracePeriodRatio: 0,
	}
	if rotateCertInterval > 0 {
		// Force cert rotation job to rotate cert.
		opt.RotationInterval = rotateCertInterval
		opt.SecretRotationGracePeriodRatio = 1.0
	}
	t.Logf("cache options: %+v", opt)
	return opt
}

// waitForCAReady makes health check requests to gRPC healthcheck service at CA server.
func (e *Env) waitForCAReady(t *testing.T) {
	conn, err := grpc.Dial(e.CAServer.URL, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("failed on connecting CA server %s: %v", e.CAServer.URL, err)
	}
	defer conn.Close()

	client := ghc.NewHealthClient(conn)
	req := new(ghc.HealthCheckRequest)
	var resp *ghc.HealthCheckResponse
	for i := 0; i < 20; i++ {
		resp, err = client.Check(context.Background(), req)
		if err == nil && resp.GetStatus() == ghc.HealthCheckResponse_SERVING {
			t.Logf("CA server is ready for handling CSR requests")
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("CA server is not ready. resp: %v, error: %v", resp, err)
}
