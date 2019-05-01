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
	"fmt"
	"net"
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
		IngressGatewayUDSPath:   "",
		WorkloadUDSPath:         fmt.Sprintf("/tmp/workload_gotest%q.sock", string(uuid.NewUUID())),
	}
	testHelper(t, arg, sdsRequestStream, false)
}

func TestStreamSecretsForGatewaySds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: true,
		EnableWorkloadSDS:       false,
		IngressGatewayUDSPath:   fmt.Sprintf("/tmp/gateway_gotest%q.sock", string(uuid.NewUUID())),
		WorkloadUDSPath:         "",
	}
	testHelper(t, arg, sdsRequestStream, false)
}

func TestStreamSecretsForBothSds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: true,
		EnableWorkloadSDS:       true,
		IngressGatewayUDSPath:   fmt.Sprintf("/tmp/gateway_gotest%q.sock", string(uuid.NewUUID())),
		WorkloadUDSPath:         fmt.Sprintf("/tmp/workload_gotest%q.sock", string(uuid.NewUUID())),
	}
	testHelper(t, arg, sdsRequestStream, false)
}

func TestFetchSecretsForWorkloadSds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: false,
		EnableWorkloadSDS:       true,
		IngressGatewayUDSPath:   "",
		WorkloadUDSPath:         fmt.Sprintf("/tmp/workload_gotest%q.sock", string(uuid.NewUUID())),
	}
	testHelper(t, arg, sdsRequestFetch, false)
}

func TestFetchSecretsForGatewaySds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: true,
		EnableWorkloadSDS:       false,
		IngressGatewayUDSPath:   fmt.Sprintf("/tmp/gateway_gotest%q.sock", string(uuid.NewUUID())),
		WorkloadUDSPath:         "",
	}
	testHelper(t, arg, sdsRequestFetch, false)
}

func TestFetchSecretsForBothSds(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: true,
		EnableWorkloadSDS:       true,
		IngressGatewayUDSPath:   fmt.Sprintf("/tmp/gateway_gotest%q.sock", string(uuid.NewUUID())),
		WorkloadUDSPath:         fmt.Sprintf("/tmp/workload_gotest%s.sock", string(uuid.NewUUID())),
	}
	testHelper(t, arg, sdsRequestFetch, false)
}

func TestStreamSecretsInvalidResourceName(t *testing.T) {
	arg := Options{
		EnableIngressGatewaySDS: false,
		EnableWorkloadSDS:       true,
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

func TestStreamSecretsPush(t *testing.T) {
	// reset connectionNumber since since its value is kept in memory for all unit test cases lifetime, reset since it may be updated in other test case.
	atomic.StoreInt64(&connectionNumber, 0)

	socket := fmt.Sprintf("/tmp/gotest%s.sock", string(uuid.NewUUID()))
	arg := Options{
		EnableIngressGatewaySDS: false,
		EnableWorkloadSDS:       true,
		WorkloadUDSPath:         socket,
	}
	st := &mockSecretStore{
		checkToken: true,
	}
	server, err := NewServer(arg, st, nil)
	defer server.Stop()
	if err != nil {
		t.Fatalf("failed to start grpc server for sds: %v", err)
	}

	proxyID := "sidecar~127.0.0.1~id2~local"
	req := &api.DiscoveryRequest{
		ResourceNames: []string{testResourceName},
		Node: &core.Node{
			Id: proxyID,
		},
	}
	// Try to call the server
	conn, err := setupConnection(socket)
	if err != nil {
		t.Errorf("failed to setup connection to socket %q", socket)
	}
	defer conn.Close()

	sdsClient := sds.NewSecretDiscoveryServiceClient(conn)
	header := metadata.Pairs(credentialTokenHeaderKey, fakeCredentialToken)
	ctx := metadata.NewOutgoingContext(context.Background(), header)
	stream, err := sdsClient.StreamSecrets(ctx)
	if err != nil {
		t.Errorf("StreamSecrets failed: %v", err)
	}
	if err = stream.Send(req); err != nil {
		t.Errorf("stream.Send failed: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Errorf("stream.Recv failed: %v", err)
	}
	verifySDSSResponse(t, resp, fakePrivateKey, fakeCertificateChain)

	// simulate logic in constructConnectionID() function.
	conID := proxyID + "-1"

	// Test push new secret to proxy.
	if err = NotifyProxy(conID, req.ResourceNames[0], &model.SecretItem{
		CertificateChain: fakePushCertificateChain,
		PrivateKey:       fakePushPrivateKey,
		ResourceName:     testResourceName,
	}); err != nil {
		t.Fatalf("failed to send push notificiation to proxy %q", conID)
	}
	resp, err = stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv failed: %v", err)
	}

	verifySDSSResponse(t, resp, fakePushPrivateKey, fakePushCertificateChain)

	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: req.ResourceNames[0],
	}
	if _, found := st.secrets.Load(key); !found {
		t.Fatalf("Failed to find cached secret")
	}

	// Test push nil secret(indicates close the streaming connection) to proxy.
	if err = NotifyProxy(conID, req.ResourceNames[0], nil); err != nil {
		t.Fatalf("failed to send push notificiation to proxy %q", conID)
	}
	if _, err = stream.Recv(); err == nil {
		t.Fatalf("stream.Recv failed, expected error")
	}

	if len(sdsClients) != 0 {
		t.Fatalf("sdsClients, got %d, expected 0", len(sdsClients))
	}
	if _, found := st.secrets.Load(key); found {
		t.Fatalf("Found cached secret after stream close, expected the secret to not exist")
	}

}

func verifySDSSResponse(t *testing.T, resp *api.DiscoveryResponse, expectedPrivateKey []byte, expectedCertChain []byte) {
	var pb authapi.Secret
	if err := types.UnmarshalAny(&resp.Resources[0], &pb); err != nil {
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
	if err := types.UnmarshalAny(&resp.Resources[0], &pb); err != nil {
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

	opts = append(opts, grpc.WithInsecure(), grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout("unix", socket, timeout)
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

func (*mockSecretStore) SecretExist(conID, spiffeID, token, version string) bool {
	return spiffeID == fakeSecret.ResourceName && token == fakeSecret.Token && version == fakeSecret.Version
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
