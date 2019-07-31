// Copyright 2018 Istio Authors
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
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	authapi "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"k8s.io/apimachinery/pkg/util/uuid"

	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/model"
)

var (
	fakeRootCert         = []byte{00}
	fakeCertificateChain = []byte{01}
	fakePrivateKey       = []byte{02}

	fakePushCertificateChain = []byte{03}
	fakePushPrivateKey       = []byte{04}

	fakeCredentialToken = "faketoken"
	testResourceName    = "default"
	extraResourceName   = "extra resource name"

	fakeSecret = &model.SecretItem{
		CertificateChain: fakeCertificateChain,
		PrivateKey:       fakePrivateKey,
		ResourceName:     testResourceName,
		Version:          time.Now().String(),
	}

	fakeSecretRootCert = &model.SecretItem{
		RootCert:     fakeRootCert,
		ResourceName: cache.RootCertReqResourceName,
		Version:      time.Now().String(),
	}
)

func TestStreamSecretsForWorkloadSds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: false,
		EnableWorkloadSDS:       true,
		RecycleInterval:         30 * time.Second,
		IngressGatewayUDSPath:   "",
		WorkloadUDSPath:         fmt.Sprintf("/tmp/workload_gotest%q.sock", string(uuid.NewUUID())),
	}
	testHelper(t, arg, sdsRequestStream, false)
}

func TestStreamSecretsForGatewaySds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: true,
		EnableWorkloadSDS:       false,
		RecycleInterval:         30 * time.Second,
		IngressGatewayUDSPath:   fmt.Sprintf("/tmp/gateway_gotest%q.sock", string(uuid.NewUUID())),
		WorkloadUDSPath:         "",
	}
	testHelper(t, arg, sdsRequestStream, false)
}

func TestStreamSecretsForBothSds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: true,
		EnableWorkloadSDS:       true,
		RecycleInterval:         30 * time.Second,
		IngressGatewayUDSPath:   fmt.Sprintf("/tmp/gateway_gotest%q.sock", string(uuid.NewUUID())),
		WorkloadUDSPath:         fmt.Sprintf("/tmp/workload_gotest%q.sock", string(uuid.NewUUID())),
	}
	testHelper(t, arg, sdsRequestStream, false)
}

func TestFetchSecretsForWorkloadSds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: false,
		EnableWorkloadSDS:       true,
		RecycleInterval:         30 * time.Second,
		IngressGatewayUDSPath:   "",
		WorkloadUDSPath:         fmt.Sprintf("/tmp/workload_gotest%q.sock", string(uuid.NewUUID())),
	}
	testHelper(t, arg, sdsRequestFetch, false)
}

func TestFetchSecretsForGatewaySds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: true,
		EnableWorkloadSDS:       false,
		RecycleInterval:         30 * time.Second,
		IngressGatewayUDSPath:   fmt.Sprintf("/tmp/gateway_gotest%q.sock", string(uuid.NewUUID())),
		WorkloadUDSPath:         "",
	}
	testHelper(t, arg, sdsRequestFetch, false)
}

func TestFetchSecretsForBothSds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: true,
		EnableWorkloadSDS:       true,
		RecycleInterval:         30 * time.Second,
		IngressGatewayUDSPath:   fmt.Sprintf("/tmp/gateway_gotest%q.sock", string(uuid.NewUUID())),
		WorkloadUDSPath:         fmt.Sprintf("/tmp/workload_gotest%s.sock", string(uuid.NewUUID())),
	}
	testHelper(t, arg, sdsRequestFetch, false)
}

func TestStreamSecretsInvalidResourceName(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: false,
		EnableWorkloadSDS:       true,
		RecycleInterval:         30 * time.Second,
		IngressGatewayUDSPath:   "",
		WorkloadUDSPath:         fmt.Sprintf("/tmp/workload_gotest%s.sock", string(uuid.NewUUID())),
	}
	testHelper(t, arg, sdsRequestStream, true)
}

type secretCallback func(string, *api.DiscoveryRequest) (*api.DiscoveryResponse, error)

func testHelper(t *testing.T, arg Options, cb secretCallback, testInvalidResourceNames bool) {
	var wst, gst cache.SecretManager
	if arg.EnableWorkloadSDS {
		wst = &mockSecretStore{
			checkToken: true,
		}
	} else {
		wst = nil
	}
	if arg.EnableIngressGatewaySDS {
		gst = &mockSecretStore{
			checkToken: false,
		}
	} else {
		gst = nil
	}
	server, err := NewServer(arg, wst, gst)
	defer server.Stop()
	if err != nil {
		t.Fatalf("failed to start grpc server for sds: %v", err)
	}

	proxyID := "sidecar~127.0.0.1~id1~local"
	if testInvalidResourceNames && arg.EnableWorkloadSDS {
		sendRequestAndVerifyResponse(t, cb, arg.WorkloadUDSPath, proxyID, testInvalidResourceNames)
		return
	}

	if arg.EnableWorkloadSDS {
		sendRequestAndVerifyResponse(t, cb, arg.WorkloadUDSPath, proxyID, testInvalidResourceNames)

		// Request for root certificate.
		sendRequestForRootCertAndVerifyResponse(t, cb, arg.WorkloadUDSPath, proxyID)
	}
	if arg.EnableIngressGatewaySDS {
		sendRequestAndVerifyResponse(t, cb, arg.IngressGatewayUDSPath, proxyID, testInvalidResourceNames)
	}
}

func sendRequestForRootCertAndVerifyResponse(t *testing.T, cb secretCallback, socket, proxyID string) {
	rootCertReq := &api.DiscoveryRequest{
		ResourceNames: []string{"ROOTCA"},
		Node: &core.Node{
			Id: proxyID,
		},
	}
	resp, err := cb(socket, rootCertReq)
	if err != nil {
		t.Fatalf("failed to get root cert through SDS")
	}
	verifySDSSResponseForRootCert(t, resp, fakeRootCert)
}

func sendRequestAndVerifyResponse(t *testing.T, cb secretCallback, socket, proxyID string, testInvalidResourceNames bool) {
	rn := []string{testResourceName}
	// Only one resource name is allowed, add extra name to create an error.
	if testInvalidResourceNames {
		rn = append(rn, extraResourceName)
	}
	req := &api.DiscoveryRequest{
		ResourceNames: rn,
		Node: &core.Node{
			Id: proxyID,
		},
	}

	wait := 300 * time.Millisecond
	retry := 0
	for ; retry < 5; retry++ {
		time.Sleep(wait)
		// Try to call the server
		resp, err := cb(socket, req)
		if testInvalidResourceNames {
			if ok := verifyResponseForInvalidResourceNames(err); ok {
				return
			}
		} else {
			if err == nil {
				//Verify secret.
				verifySDSSResponse(t, resp, fakePrivateKey, fakeCertificateChain)
				return
			}
		}
		wait *= 2
	}

	if retry == 5 {
		t.Fatal("failed to start grpc server for SDS")
	}
}

func verifyResponseForInvalidResourceNames(err error) bool {
	s := fmt.Sprintf("has invalid resourceNames [%s %s]", testResourceName, extraResourceName)
	return strings.Contains(err.Error(), s)
}

func testSDSStreamTwo(t *testing.T, stream sds.SecretDiscoveryService_StreamSecretsClient, proxyID string,
	notifyChan chan string) {
	req := &api.DiscoveryRequest{
		ResourceNames: []string{testResourceName},
		Node: &core.Node{
			Id: proxyID,
		},
	}
	if err := stream.Send(req); err != nil {
		t.Errorf("stream.Send failed: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Errorf("stream.Recv failed: %v", err)
	}
	verifySDSSResponse(t, resp, fakePrivateKey, fakeCertificateChain)
	notifyChan <- "close stream"
}

func createSDSServer(t *testing.T, socket string) (*Server, *mockSecretStore) {
	arg := Options{
		EnableIngressGatewaySDS: false,
		EnableWorkloadSDS:       true,
		RecycleInterval:         2 * time.Second,
		WorkloadUDSPath:         socket,
	}
	st := &mockSecretStore{
		checkToken: false,
	}
	server, err := NewServer(arg, st, nil)
	if err != nil {
		t.Fatalf("failed to start grpc server for sds: %v", err)
	}
	return server, st
}

func createSDSStream(t *testing.T, socket string) (*grpc.ClientConn, sds.SecretDiscoveryService_StreamSecretsClient) {
	// Try to call the server
	conn, err := setupConnection(socket)
	if err != nil {
		t.Errorf("failed to setup connection to socket %q", socket)
	}
	sdsClient := sds.NewSecretDiscoveryServiceClient(conn)
	header := metadata.Pairs(credentialTokenHeaderKey, "")
	ctx := metadata.NewOutgoingContext(context.Background(), header)
	stream, err := sdsClient.StreamSecrets(ctx)
	if err != nil {
		t.Errorf("StreamSecrets failed: %v", err)
	}
	return conn, stream
}

func testSDSStreamOne(t *testing.T, stream sds.SecretDiscoveryService_StreamSecretsClient, proxyID string,
	notifyChan chan string) {
	req := &api.DiscoveryRequest{
		ResourceNames: []string{testResourceName},
		Node: &core.Node{
			Id: proxyID,
		},
	}

	// Send first request and
	if err := stream.Send(req); err != nil {
		t.Errorf("stream.Send failed: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Errorf("stream.Recv failed: %v", err)
	}
	verifySDSSResponse(t, resp, fakePrivateKey, fakeCertificateChain)

	// Send second request as an ACK and wait for notifyPush
	req.VersionInfo = resp.VersionInfo
	req.ResponseNonce = resp.Nonce
	if err = stream.Send(req); err != nil {
		t.Errorf("stream.Send failed: %v", err)
	}
	// Wait for SDS server to process the second request and hold the request.
	time.Sleep(5 * time.Second)
	notifyChan <- "notify push secret"
	if notify := <-notifyChan; notify == "receive secret" {
		resp, err = stream.Recv()
		if err != nil {
			t.Errorf("stream.Recv failed: %v", err)
		}
		verifySDSSResponse(t, resp, fakePushPrivateKey, fakePushCertificateChain)
	}

	// Send third request as an ACK and wait for stream close
	req.VersionInfo = resp.VersionInfo
	req.ResponseNonce = resp.Nonce
	if err = stream.Send(req); err != nil {
		t.Errorf("stream.Send failed: %v", err)
	}
	// Wait for SDS server to process the second request and hold the request.
	time.Sleep(5 * time.Second)
	notifyChan <- "notify push secret"
	if notify := <-notifyChan; notify == "receive nil secret" {
		resp, err = stream.Recv()
		if err == nil {
			t.Errorf("stream.Recv should fail, expected error but got %+v", resp)
		}
	}
	notifyChan <- "close stream"
}

func TestStreamSecretsPush(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/15923")
	// reset connectionNumber since since its value is kept in memory for all unit test cases lifetime, reset since it may be updated in other test case.
	atomic.StoreInt64(&connectionNumber, 0)

	socket := fmt.Sprintf("/tmp/gotest%s.sock", string(uuid.NewUUID()))
	server, st := createSDSServer(t, socket)
	defer server.Stop()

	connOne, streamOne := createSDSStream(t, socket)
	proxyID := "sidecar~127.0.0.1~id2~local"
	notifyChan := make(chan string)
	go testSDSStreamOne(t, streamOne, proxyID, notifyChan)

	connTwo, streamTwo := createSDSStream(t, socket)
	proxyIDTwo := "sidecar~127.0.0.1~id3~local"
	notifyChanTwo := make(chan string)
	go testSDSStreamTwo(t, streamTwo, proxyIDTwo, notifyChanTwo)

	if notify := <-notifyChan; notify != "notify push secret" {
		t.Fatalf("push signal does not match")
	}
	// simulate logic in constructConnectionID() function.
	conID := proxyID + "-1"
	pushSecret := &model.SecretItem{
		CertificateChain: fakePushCertificateChain,
		PrivateKey:       fakePushPrivateKey,
		ResourceName:     testResourceName,
		Version:          time.Now().String(),
	}
	// Test push new secret to proxy.
	if err := NotifyProxy(cache.ConnKey{ConnectionID: conID, ResourceName: testResourceName},
		pushSecret); err != nil {
		t.Fatalf("failed to send push notificiation to proxy %q", conID)
	}
	// load pushed secret into cache, this is needed to detect an ACK request.
	st.secrets.Store(cache.ConnKey{ConnectionID: conID, ResourceName: testResourceName}, pushSecret)
	notifyChan <- "receive secret"

	// Verify that pushed secret is stored in cache.
	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: testResourceName,
	}
	if _, found := st.secrets.Load(key); !found {
		t.Fatalf("Failed to find cached secret")
	}

	if notify := <-notifyChan; notify != "notify push secret" {
		t.Fatalf("push signal does not match")
	}
	// Test push nil secret(indicates close the streaming connection) to proxy.
	if err := NotifyProxy(cache.ConnKey{ConnectionID: conID, ResourceName: testResourceName}, nil); err != nil {
		t.Fatalf("failed to send push notificiation to proxy %q", conID)
	}
	notifyChan <- "receive nil secret"

	if notify := <-notifyChan; notify != "close stream" {
		t.Fatalf("get unexpected notification. %s", notify)
	}
	connOne.Close()
	if notify := <-notifyChanTwo; notify != "close stream" {
		t.Fatalf("get unexpected notification. %s", notify)
	}
	connTwo.Close()

	if _, found := st.secrets.Load(key); found {
		t.Fatalf("Found cached secret after stream close, expected the secret to not exist")
	}
	// Wait the recycle job run to clear all staled client connections.
	time.Sleep(10 * time.Second)

	// Add RLock to avoid racetest fail.
	sdsClientsMutex.RLock()
	defer sdsClientsMutex.RUnlock()
	if len(sdsClients) != 0 {
		t.Fatalf("sdsClients, got %d, expected 0", len(sdsClients))
	}
}

func testSDSStreamMultiplePush(t *testing.T, stream sds.SecretDiscoveryService_StreamSecretsClient, proxyID string,
	notifyChan chan string) {
	req := &api.DiscoveryRequest{
		ResourceNames: []string{testResourceName},
		Node: &core.Node{
			Id: proxyID,
		},
	}

	// Send first request and
	if err := stream.Send(req); err != nil {
		t.Errorf("stream.Send failed: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Errorf("stream.Recv failed: %v", err)
	}
	verifySDSSResponse(t, resp, fakePrivateKey, fakeCertificateChain)

	notifyChan <- "notify push secret"
	if notify := <-notifyChan; notify == "receive secret" {
		// Verify that Recv() does not receive secret push and returns when stream is closed.
		_, err = stream.Recv()
		if err == nil {
			t.Errorf("stream.Recv should fail: %v", err)
		}
		if !strings.Contains(err.Error(), "the client connection is closing") {
			t.Errorf("received error does not match, got %v", err)
		}
	}
}

// TestStreamSecretsMultiplePush verifies that only one response is pushed per request, and that multiple
// pushes are detected and skipped.
func TestStreamSecretsMultiplePush(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/15923")
	// reset connectionNumber since since its value is kept in memory for all unit test cases lifetime, reset since it may be updated in other test case.
	atomic.StoreInt64(&connectionNumber, 0)

	socket := fmt.Sprintf("/tmp/gotest%s.sock", string(uuid.NewUUID()))
	server, _ := createSDSServer(t, socket)
	defer server.Stop()

	conn, stream := createSDSStream(t, socket)
	proxyID := "sidecar~127.0.0.1~id2~local"
	defer conn.Close()
	notifyChan := make(chan string)
	go testSDSStreamMultiplePush(t, stream, proxyID, notifyChan)

	if notify := <-notifyChan; notify != "notify push secret" {
		t.Fatalf("push signal does not match")
	}

	// simulate logic in constructConnectionID() function.
	conID := proxyID + "-1"
	pushSecret := &model.SecretItem{
		CertificateChain: fakePushCertificateChain,
		PrivateKey:       fakePushPrivateKey,
		ResourceName:     testResourceName,
		Version:          time.Now().String(),
	}
	// Test push new secret to proxy.
	if err := NotifyProxy(cache.ConnKey{ConnectionID: conID, ResourceName: testResourceName},
		pushSecret); err != nil {
		t.Fatalf("failed to send push notificiation to proxy %q", conID)
	}
	notifyChan <- "receive secret"
}

func verifySDSSResponse(t *testing.T, resp *api.DiscoveryResponse, expectedPrivateKey []byte, expectedCertChain []byte) {
	var pb authapi.Secret
	if err := types.UnmarshalAny(resp.Resources[0], &pb); err != nil {
		t.Fatalf("UnmarshalAny SDS response failed: %v", err)
	}

	expectedResponseSecret := authapi.Secret{
		Name: testResourceName,
		Type: &authapi.Secret_TlsCertificate{
			TlsCertificate: &authapi.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: expectedCertChain,
					},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: expectedPrivateKey,
					},
				},
			},
		},
	}
	if !reflect.DeepEqual(pb, expectedResponseSecret) {
		t.Errorf("secret key: got %+v, want %+v", pb, expectedResponseSecret)
	}
}

func verifySDSSResponseForRootCert(t *testing.T, resp *api.DiscoveryResponse, expectedRootCert []byte) {
	var pb authapi.Secret
	if err := types.UnmarshalAny(resp.Resources[0], &pb); err != nil {
		t.Fatalf("UnmarshalAny SDS response failed: %v", err)
	}

	expectedResponseSecret := authapi.Secret{
		Name: "ROOTCA",
		Type: &authapi.Secret_ValidationContext{
			ValidationContext: &authapi.CertificateValidationContext{
				TrustedCa: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: expectedRootCert,
					},
				},
			},
		},
	}
	if !reflect.DeepEqual(pb, expectedResponseSecret) {
		t.Errorf("secret key: got %+v, want %+v", pb, expectedResponseSecret)
	}
}

func sdsRequestStream(socket string, req *api.DiscoveryRequest) (*api.DiscoveryResponse, error) {
	conn, err := setupConnection(socket)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	sdsClient := sds.NewSecretDiscoveryServiceClient(conn)
	header := metadata.Pairs(credentialTokenHeaderKey, fakeCredentialToken)
	ctx := metadata.NewOutgoingContext(context.Background(), header)
	stream, err := sdsClient.StreamSecrets(ctx)
	if err != nil {
		return nil, err
	}
	err = stream.Send(req)
	if err != nil {
		return nil, err
	}
	res, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	return res, nil
}

func sdsRequestFetch(socket string, req *api.DiscoveryRequest) (*api.DiscoveryResponse, error) {
	conn, err := setupConnection(socket)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	sdsClient := sds.NewSecretDiscoveryServiceClient(conn)
	header := metadata.Pairs(credentialTokenHeaderKey, fakeCredentialToken)
	ctx := metadata.NewOutgoingContext(context.Background(), header)
	resp, err := sdsClient.FetchSecrets(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func setupConnection(socket string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithInsecure(), grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}))

	conn, err := grpc.Dial(socket, opts...)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

type mockSecretStore struct {
	checkToken bool
	secrets    sync.Map
}

func (ms *mockSecretStore) GenerateSecret(ctx context.Context, conID, resourceName, token string) (*model.SecretItem, error) {
	if ms.checkToken && token != fakeCredentialToken {
		return nil, fmt.Errorf("unexpected token %q", token)
	}

	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: resourceName,
	}
	if resourceName == testResourceName {
		ms.secrets.Store(key, fakeSecret)
		return fakeSecret, nil
	}

	if resourceName == cache.RootCertReqResourceName {
		ms.secrets.Store(key, fakeSecretRootCert)
		return fakeSecretRootCert, nil
	}

	return nil, fmt.Errorf("unexpected resourceName %q", resourceName)
}

func (ms *mockSecretStore) SecretExist(conID, spiffeID, token, version string) bool {
	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: spiffeID,
	}
	val, found := ms.secrets.Load(key)
	if !found {
		fmt.Printf("cannot find secret in cache")
		return false
	}
	cs := val.(*model.SecretItem)
	if spiffeID != cs.ResourceName {
		fmt.Printf("resource name not match: %s vs %s", spiffeID, cs.ResourceName)
		return false
	}
	if token != cs.Token {
		fmt.Printf("token does not match %+v vs %+v", token, cs.Token)
		return false
	}
	if version != cs.Version {
		fmt.Printf("version does not match %s vs %s", version, cs.Version)
		return false
	}
	fmt.Printf("requested secret matches cache")
	return true
}

func (ms *mockSecretStore) DeleteSecret(conID, resourceName string) {
	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: resourceName,
	}
	ms.secrets.Delete(key)
}

func (ms *mockSecretStore) ShouldWaitForIngressGatewaySecret(connectionID, resourceName, token string) bool {
	return false
}

func TestDebugEndpoints(t *testing.T) {

	tests := []struct {
		proxies []string
	}{
		{proxies: []string{}},
		{proxies: []string{"sidecar~127.0.0.1~id2~local", "sidecar~127.0.0.1~id3~local"}},
		{proxies: []string{"sidecar~127.0.0.1~id4~local"}},
	}

	for _, tc := range tests {
		socket := fmt.Sprintf("/tmp/gotest%s.sock", string(uuid.NewUUID()))
		arg := Options{
			EnableIngressGatewaySDS: false,
			EnableWorkloadSDS:       true,
			RecycleInterval:         2 * time.Second,
			WorkloadUDSPath:         socket,
		}
		st := &mockSecretStore{
			checkToken: true,
		}

		sdsClients = map[cache.ConnKey]*sdsConnection{}
		server, err := NewServer(arg, st, nil)
		if err != nil {
			t.Fatalf("failed to start grpc server for sds: %v", err)
		}

		for _, proxy := range tc.proxies {
			sendRequestAndVerifyResponse(t, sdsRequestStream, arg.WorkloadUDSPath, proxy, false)
		}

		workloadRequest, _ := http.NewRequest(http.MethodGet, "/debug/sds/workload", nil)
		response := httptest.NewRecorder()

		server.workloadSds.debugHTTPHandler(response, workloadRequest)
		workloadDebugResponse := &sdsdebug{}
		if err := json.Unmarshal(response.Body.Bytes(), workloadDebugResponse); err != nil {
			t.Fatalf("debug JSON unmarshalling failed: %v", err)
		}

		clientCount := len(workloadDebugResponse.Clients)
		if clientCount != len(tc.proxies) {
			t.Errorf("response should contain %d client, found %d", len(tc.proxies), clientCount)
		}

		// check whether debug endpoint returned the registered proxies
		for _, p := range tc.proxies {
			found := false
			for _, c := range workloadDebugResponse.Clients {
				if p == c.ProxyID {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("expected to debug endpoint to contain %s, but did not", p)
			}
		}

		server.Stop()
	}
}

// make helper function that takes (proxyname, socket, opts) and makes DiscoveryRequest
// figure out what a discovery request is and if it's needed to become a client, even?
// confirmed from L236 that disc req is where the addClient gets called
