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
	"testing"
	"time"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	authapi "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
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

func TestStreamSecrets(t *testing.T) {
	socket := fmt.Sprintf("/tmp/gotest%q.sock", string(uuid.NewUUID()))
	testHelper(t, socket, sdsRequestStream)
}

func TestFetchSecrets(t *testing.T) {
	socket := fmt.Sprintf("/tmp/gotest%s.sock", string(uuid.NewUUID()))
	testHelper(t, socket, sdsRequestFetch)
}

type secretCallback func(string, *api.DiscoveryRequest) (*api.DiscoveryResponse, error)

func testHelper(t *testing.T, testSocket string, cb secretCallback) {
	arg := Options{
		UDSPath: testSocket,
	}
	st := &mockSecretStore{}
	server, err := NewServer(arg, st)
	defer server.Stop()

	if err != nil {
		t.Fatalf("failed to start grpc server for sds: %v", err)
	}

	proxyID := "sidecar~127.0.0.1~id1~local"
	req := &api.DiscoveryRequest{
		ResourceNames: []string{testResourceName},
		Node: &core.Node{
			Id: proxyID,
		},
	}

	wait := 300 * time.Millisecond
	retry := 0
	for ; retry < 5; retry++ {
		time.Sleep(wait)
		// Try to call the server
		resp, err := cb(testSocket, req)
		if err == nil {
			//Verify secret.
			verifySDSSResponse(t, resp, fakePrivateKey, fakeCertificateChain)
			return
		}

		wait *= 2
	}

	if retry == 5 {
		t.Fatalf("failed to start grpc server for SDS")
	}

	// Request for root certificate.
	rootCertReq := &api.DiscoveryRequest{
		ResourceNames: []string{"ROOTCA", "sub1"},
		Node: &core.Node{
			Id: proxyID,
		},
	}
	resp, err := cb(testSocket, rootCertReq)
	if err != nil {
		t.Fatalf("failed to get root cert through SDS")
	}
	verifySDSSResponseForRootCert(t, resp, fakeRootCert)
}

func TestStreamSecretsPush(t *testing.T) {
	socket := fmt.Sprintf("/tmp/gotest%s.sock", string(uuid.NewUUID()))
	arg := Options{
		UDSPath: socket,
	}
	st := &mockSecretStore{}
	server, err := NewServer(arg, st)
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
	header := metadata.Pairs(CredentialTokenHeaderKey, fakeCredentialToken)
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

	// Test push new secret to proxy.
	if err = NotifyProxy(proxyID, req.ResourceNames[0], &model.SecretItem{
		CertificateChain: fakePushCertificateChain,
		PrivateKey:       fakePushPrivateKey,
		ResourceName:     testResourceName,
	}); err != nil {
		t.Errorf("failed to send push notificiation to proxy %q", proxyID)
	}
	resp, err = stream.Recv()
	if err != nil {
		t.Errorf("stream.Recv failed: %v", err)
	}

	verifySDSSResponse(t, resp, fakePushPrivateKey, fakePushCertificateChain)

	// Test push nil secret(indicates close the streaming connection) to proxy.
	if err = NotifyProxy(proxyID, req.ResourceNames[0], nil); err != nil {
		t.Errorf("failed to send push notificiation to proxy %q", proxyID)
	}
	if _, err = stream.Recv(); err == nil {
		t.Errorf("stream.Recv failed, expected error")
	}

	if len(sdsClients) != 0 {
		t.Errorf("sdsClients, got %d, expected 0", len(sdsClients))
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
				VerifySubjectAltName: []string{"sub1"},
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
	header := metadata.Pairs(CredentialTokenHeaderKey, fakeCredentialToken)
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
	header := metadata.Pairs(CredentialTokenHeaderKey, fakeCredentialToken)
	ctx := metadata.NewOutgoingContext(context.Background(), header)
	resp, err := sdsClient.FetchSecrets(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func setupConnection(socket string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithInsecure())
	opts = append(opts, grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout("unix", socket, timeout)
	}))

	conn, err := grpc.Dial(socket, opts...)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

type mockSecretStore struct {
}

func (*mockSecretStore) GenerateSecret(ctx context.Context, proxyID, resourceName, token string) (*model.SecretItem, error) {
	if token != fakeCredentialToken {
		return nil, fmt.Errorf("unexpected token %q", token)
	}

	if resourceName == testResourceName {
		return fakeSecret, nil
	}

	if resourceName == cache.RootCertReqResourceName {
		return fakeSecretRootCert, nil
	}

	return nil, fmt.Errorf("unexpected resourceName %q", resourceName)
}

func (*mockSecretStore) SecretExist(proxyID, spiffeID, token, version string) bool {
	return spiffeID == fakeSecret.ResourceName && token == fakeSecret.Token && version == fakeSecret.Version
}
