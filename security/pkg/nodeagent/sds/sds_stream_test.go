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
package sds

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"k8s.io/apimachinery/pkg/util/uuid"

	"istio.io/istio/pkg/security"

	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/util"
)

const (
	ValidProxyID        = "sidecar~127.0.0.1~SecretsPushStreamOne~local"
	InValidProxyID      = "invalid~sidecar~127.0.0.1~SecretsPushStreamOne~local"
	BlockGenSecretError = "BLOCK_GENERATE_SECRET_FOR_TEST_ERROR"
)

/*
	Validate that secrets are cleaned after connection is closed in such two cases:
	1. workflow are run completely and connection is closed
	2. workflow are terminated and connection is closed  after secret are generated
*/
func TestSDSAgentStreamWithCacheAndConnectionCleaned(t *testing.T) {
	setup := StartStreamTest(t)
	defer setup.server.Stop()

	conn, stream := createSDSStream(t, setup.socket, fakeToken1)
	notifyChan := make(chan notifyMsg)

	go testSDSSuccessIngressStreamCache(stream, ValidProxyID, notifyChan)
	// verify that the first SDS request sent by two streams do not hit cache.
	waitForStreamSecretCacheCheck(t, setup.secretStore, false, 1)
	waitForStreamNotificationToProceed(t, notifyChan, "notify push secret 1")
	secretKeyMap := make(map[interface{}]bool)
	setup.secretStore.secrets.Range(func(key, value interface{}) bool {
		secretKeyMap[key] = true
		return false
	})
	conn.Close()
	// verify the cache is cleaned when connection is closed
	waitForSecretCacheCleanUp(t, setup.secretStore, secretKeyMap)

	conn, stream = createSDSStream(t, setup.socket, fakeToken1)
	// When proxy ID has "invalid", SDS server closes the connection and returns an error
	go testSDSTerminatedIngressStreamCache(stream, InValidProxyID, notifyChan)
	waitForStreamSecretCacheCheck(t, setup.secretStore, false, 1)
	waitForStreamNotificationToProceed(t, notifyChan, "notify push secret 2")

	secretKeyMap = make(map[interface{}]bool)
	setup.secretStore.secrets.Range(func(key, value interface{}) bool {
		secretKeyMap[key] = true
		return false
	})
	conn.Close()
	// verify the cache is cleaned when connection is closed
	waitForSecretCacheCleanUp(t, setup.secretStore, secretKeyMap)
}

func waitForSecretCacheCleanUp(t *testing.T, secretsStore *mockIngressGatewaySecretStore, secretMap map[interface{}]bool) {
	waitTimeout := 2 * time.Second
	start := time.Now()
	for {
		cacheNotCleaned := false
		secretsStore.secrets.Range(func(key, value interface{}) bool {
			_, found := secretMap[key]
			if found {
				cacheNotCleaned = true
			}
			return false
		})
		if !cacheNotCleaned {
			return
		}
		if time.Since(start) > waitTimeout && cacheNotCleaned {
			t.Fatalf("cache is not cleaned")
			return
		}
		time.Sleep(1 * time.Second)
	}
}

// waitForSecretCacheCheck wait until cache hit or cache miss meets expected value and return. Or
// return directly on timeout.
func waitForStreamSecretCacheCheck(t *testing.T, mss *mockIngressGatewaySecretStore, expectCacheHit bool, expectValue int) {
	waitTimeout := 5 * time.Second
	checkMetric := "cache hit"
	if !expectCacheHit {
		checkMetric = "cache miss"
	}
	realVal := 0
	start := time.Now()
	for {
		if expectCacheHit {
			realVal = mss.SecretCacheHit()
			if realVal == expectValue {
				return
			}
		}
		if !expectCacheHit {
			realVal = mss.SecretCacheMiss()
			if realVal == expectValue {
				return
			}
		}
		if time.Since(start) > waitTimeout {
			t.Fatalf("%s does not meet expected value in %s, expected %d but got %d",
				checkMetric, waitTimeout.String(), expectValue, realVal)
			return
		}
		time.Sleep(1 * time.Second)
	}
}

// workflow are run completely
func testSDSSuccessIngressStreamCache(stream sds.SecretDiscoveryService_StreamSecretsClient, proxyID string,
	notifyChan chan notifyMsg) {
	req := &discovery.DiscoveryRequest{
		TypeUrl:       SecretTypeV3,
		ResourceNames: []string{testResourceName},
		Node: &core.Node{
			Id: proxyID,
		},
		// Set a non-empty version info so that StreamSecrets() starts a cache check, and cache miss
		// metric is updated accordingly.
		VersionInfo: "initial_version",
	}
	// Send first request and verify response
	if err := stream.Send(req); err != nil {
		notifyChan <- notifyMsg{Err: err, Message: fmt.Sprintf("stream one: stream.Send failed: %v", err)}
	}

	resp, err := stream.Recv()
	if err != nil {
		notifyChan <- notifyMsg{Err: err, Message: fmt.Sprintf("stream one: stream.Recv failed: %v", err)}
	}
	if err := verifySDSSResponse(resp, fakePrivateKey, fakeCertificateChain); err != nil {
		notifyChan <- notifyMsg{Err: err, Message: fmt.Sprintf(
			"stream one: first SDS response verification failed: %v", err)}
	}
	notifyChan <- notifyMsg{Err: nil, Message: "notify push secret 1"}
}

// workflow are terminated after secret are generated
func testSDSTerminatedIngressStreamCache(stream sds.SecretDiscoveryService_StreamSecretsClient, proxyID string,
	notifyChan chan notifyMsg) {
	req := &discovery.DiscoveryRequest{
		TypeUrl:       SecretTypeV3,
		ResourceNames: []string{testResourceName},
		Node: &core.Node{
			Id: proxyID,
		},
		// Set a non-empty version info so that StreamSecrets() starts a cache check, and cache miss
		// metric is updated accordingly.
		VersionInfo: "initial_version",
	}
	// Send first request and verify response
	if err := stream.Send(req); err != nil {
		notifyChan <- notifyMsg{Err: err, Message: fmt.Sprintf("stream: stream.Send failed: %v", err)}
	}
	_, err := stream.Recv()
	if err != nil {
		notifyChan <- notifyMsg{Err: err, Message: BlockGenSecretError}
	}
	notifyChan <- notifyMsg{Err: nil, Message: "notify push secret 2"}
}

type StreamSetup struct {
	t                          *testing.T
	socket                     string
	server                     *Server
	secretStore                *mockIngressGatewaySecretStore
	initialTotalPush           float64
	initialTotalUpdateFailures float64
}

func (s *StreamSetup) waitForSDSReady() error {
	var conErr, streamErr error
	var conn *grpc.ClientConn
	for i := 0; i < 20; i++ {
		if conn, conErr = setupConnection(s.socket); conErr == nil {
			sdsClient := sds.NewSecretDiscoveryServiceClient(conn)
			header := metadata.Pairs(credentialTokenHeaderKey, fakeToken1)
			ctx := metadata.NewOutgoingContext(context.Background(), header)
			if _, streamErr = sdsClient.StreamSecrets(ctx); streamErr == nil {
				return nil
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("cannot connect SDS server, connErr: %v, streamErr: %v", conErr, streamErr)
}

type mockIngressGatewaySecretStore struct {
	checkToken      bool
	secrets         sync.Map
	mutex           sync.RWMutex
	secretCacheHit  int
	secretCacheMiss int
}

func (ms *mockIngressGatewaySecretStore) SecretCacheHit() int {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()
	return ms.secretCacheHit
}

func (ms *mockIngressGatewaySecretStore) SecretCacheMiss() int {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()
	return ms.secretCacheMiss
}

func (ms *mockIngressGatewaySecretStore) GenerateSecret(ctx context.Context, conID, resourceName, token string) (*security.SecretItem, error) {
	if ms.checkToken && token != fakeToken1 && token != fakeToken2 {
		return nil, fmt.Errorf("unexpected token %q", token)
	}

	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: resourceName,
	}
	if resourceName == testResourceName {
		s := &security.SecretItem{
			CertificateChain: fakeCertificateChain,
			PrivateKey:       fakePrivateKey,
			ResourceName:     testResourceName,
			Version:          time.Now().Format("01-02 15:04:05.000"),
			Token:            token,
		}
		fmt.Println("Store secret for key: ", key, ". token: ", token)
		ms.secrets.Store(key, s)
		// if it is the invalid connection for test,
		// we still store the secret to the cache, however we will throw an error here
		// to earlier terminate the flow and then test whether the secret will be deleted
		// after connection is stopped

		if strings.Contains(conID, "invalid") {
			return s, fmt.Errorf("invalid connection for test")
		}
		return s, nil
	}

	return nil, fmt.Errorf("unexpected resourceName %q", resourceName)
}

func (ms *mockIngressGatewaySecretStore) SecretExist(conID, spiffeID, token, version string) bool {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: spiffeID,
	}
	val, found := ms.secrets.Load(key)
	if !found {
		fmt.Printf("cannot find secret %v in cache\n", key)
		ms.secretCacheMiss++
		return false
	}
	cs := val.(*security.SecretItem)
	fmt.Println("key is: ", key, ". Token: ", cs.Token)
	if spiffeID != cs.ResourceName {
		fmt.Printf("resource name not match: %s vs %s\n", spiffeID, cs.ResourceName)
		ms.secretCacheMiss++
		return false
	}
	if token != cs.Token {
		fmt.Printf("token does not match %+v vs %+v\n", token, cs.Token)
		ms.secretCacheMiss++
		return false
	}
	if version != cs.Version {
		fmt.Printf("version does not match %s vs %s\n", version, cs.Version)
		ms.secretCacheMiss++
		return false
	}
	fmt.Printf("requested secret matches cache\n")
	ms.secretCacheHit++
	return true
}

func (ms *mockIngressGatewaySecretStore) DeleteSecret(conID, resourceName string) {
	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: resourceName,
	}
	fmt.Printf("mockIngressGatewaySecretStore,conId: %s, resourceName: %s", conID, resourceName)
	ms.secrets.Delete(key)
}

func (ms *mockIngressGatewaySecretStore) ShouldWaitForGatewaySecret(connectionID, resourceName, token string, fileMountedCertsOnly bool) bool {
	return false
}

// StartStreamTest starts SDS server and checks SDS connectivity.
func StartStreamTest(t *testing.T) *StreamSetup {
	s := &StreamSetup{t: t}
	// reset connectionNumber since since its value is kept in memory for all unit test cases lifetime,
	// reset since it may be updated in other test case.
	atomic.StoreInt64(&connectionNumber, 0)

	s.socket = fmt.Sprintf("/tmp/gotest%s.sock", string(uuid.NewUUID()))
	s.server, s.secretStore = createStreamSDSServer(t, s.socket)

	if err := s.waitForSDSReady(); err != nil {
		t.Fatalf("fail to start SDS server: %v", err)
	}

	// Get initial SDS push stats.
	initialTotalPush, err := util.GetMetricsCounterValue("total_pushes")
	if err != nil {
		t.Fatalf("fail to get initial value from metric totalPush: %v", err)
	}
	initialTotalUpdateFailures, err := util.GetMetricsCounterValue("total_secret_update_failures")
	if err != nil {
		t.Fatalf("fail to get initial value from metric totalSecretUpdateFailureCounts: %v", err)
	}
	s.initialTotalPush = initialTotalPush
	s.initialTotalUpdateFailures = initialTotalUpdateFailures

	return s
}

func createStreamSDSServer(t *testing.T, socket string) (*Server, *mockIngressGatewaySecretStore) {
	arg := security.Options{
		EnableGatewaySDS:  false,
		EnableWorkloadSDS: true,
		RecycleInterval:   100 * time.Second,
		WorkloadUDSPath:   socket,
	}
	st := &mockIngressGatewaySecretStore{
		checkToken: false,
	}
	server, err := NewServer(&arg, st, nil)
	if err != nil {
		t.Fatalf("failed to start grpc server for sds: %v", err)
	}
	return server, st
}

func waitForStreamNotificationToProceed(t *testing.T, notifyChan chan notifyMsg, proceedNotice string) {
	for {
		if notify := <-notifyChan; notify.Err != nil {
			if notify.Message == BlockGenSecretError {
				return
			}
			t.Fatalf("get error from stream: %v", notify.Message)
		} else {
			if notify.Message != proceedNotice {
				t.Fatalf("push signal does not match, expected %s but got %s", proceedNotice,
					notify.Message)
			}
			return
		}
	}
}
