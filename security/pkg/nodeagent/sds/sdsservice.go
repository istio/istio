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

// Package sds implements secret discovery service in NodeAgent.
package sds

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	authapi "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/model"
)

const (
	// SecretType is used for secret discovery service to construct response.
	SecretType = "type.googleapis.com/envoy.api.v2.auth.Secret"

	// credentialTokenHeaderKey is the header key in gPRC header which is used to
	// pass credential token from envoy's SDS request to SDS service.
	credentialTokenHeaderKey = "authorization"

	// k8sSAJwtTokenHeaderKey is the request header key, header value is k8s sa jwt, which is set in
	// https://github.com/istio/istio/blob/master/pilot/pkg/model/authentication.go
	k8sSAJwtTokenHeaderKey = "istio_sds_credentail_header-bin"

	// IngressGatewaySdsCaSuffix is the suffix of the sds resource name for root CA. All SDS requests
	// for root CA sent by ingress gateway have suffix -cacert.
	IngressGatewaySdsCaSuffix = "-cacert"
)

var (
	sdsClients      = map[cache.ConnKey]*sdsConnection{}
	sdsClientsMutex sync.RWMutex

	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)
)

type discoveryStream interface {
	Send(*xdsapi.DiscoveryResponse) error
	Recv() (*xdsapi.DiscoveryRequest, error)
	grpc.ServerStream
}

// sdsEvent represents a secret event that results in a push.
type sdsEvent struct{}

type sdsConnection struct {
	// Time of connection, for debugging.
	Connect time.Time

	// The ID of proxy from which the connection comes from.
	proxyID string

	// The ResourceName of the SDS request.
	ResourceName string

	// Sending on this channel results in  push.
	pushChannel chan *sdsEvent

	// SDS streams implement this interface.
	stream discoveryStream

	// The secret associated with the proxy.
	secret *model.SecretItem

	// Mutex to protect read/write to this connection
	mutex sync.RWMutex

	// ConID is the connection identifier, used as a key in the connection table.
	// Currently based on the node name and a counter.
	conID string
}

type sdsservice struct {
	st cache.SecretManager
	// skipToken indicates whether token is required.
	skipToken bool
}

// newSDSService creates Secret Discovery Service which implements envoy v2 SDS API.
func newSDSService(st cache.SecretManager, skipTokenVerification bool) *sdsservice {
	if st == nil {
		return nil
	}

	return &sdsservice{
		st:        st,
		skipToken: skipTokenVerification,
	}
}

// register adds the SDS handle to the grpc server
func (s *sdsservice) register(rpcs *grpc.Server) {
	sds.RegisterSecretDiscoveryServiceServer(rpcs, s)
}

func (s *sdsservice) StreamSecrets(stream sds.SecretDiscoveryService_StreamSecretsServer) error {
	token := ""
	var ctx context.Context
	if !s.skipToken {
		ctx = stream.Context()
		t, err := getCredentialToken(ctx)
		if err != nil {
			log.Errorf("Failed to get credential token from incoming request: %v", err)
			return err
		}
		token = t
	}

	var receiveError error
	reqChannel := make(chan *xdsapi.DiscoveryRequest, 1)
	con := newSDSConnection(stream)

	go receiveThread(con, reqChannel, &receiveError)

	for {
		// Block until a request is received.
		select {
		case discReq, ok := <-reqChannel:
			if !ok {
				// Remote side closed connection.
				return receiveError
			}

			if discReq.Node == nil {
				log.Errorf("Invalid discovery request with no node")
				return fmt.Errorf("invalid discovery request with no node")
			}

			resourceName, err := parseDiscoveryRequest(discReq)
			if err != nil {
				log.Errorf("Failed to parse discovery request: %v", err)
				return err
			}

			con.proxyID = discReq.Node.Id
			con.ResourceName = resourceName

			key := cache.ConnKey{
				ResourceName: resourceName,
			}

			var firstRequestFlag bool
			con.mutex.Lock()
			if con.conID == "" {
				// first request
				con.conID = constructConnectionID(discReq.Node.Id)
				key.ConnectionID = con.conID
				addConn(key, con)
				firstRequestFlag = true
			}
			con.mutex.Unlock()

			// When nodeagent receives StreamSecrets request, if there is cached secret which matches
			// request's <token, resourceName, Version>, then this request is a confirmation request.
			// nodeagent stops sending response to envoy in this case.
			if discReq.VersionInfo != "" && s.st.SecretExist(con.conID, resourceName, token, discReq.VersionInfo) {
				log.Debugf("Received SDS ACK from %q, connectionID %q, resourceName %q, versionInfo %q\n", discReq.Node.Id, con.conID, resourceName, discReq.VersionInfo)
				continue
			}

			if firstRequestFlag {
				log.Debugf("Received first SDS request from %q, connectionID %q, resourceName %q, versionInfo %q\n",
					discReq.Node.Id, con.conID, resourceName, discReq.VersionInfo)
			} else {
				log.Debugf("Received SDS request from %q, connectionID %q, resourceName %q, versionInfo %q\n",
					discReq.Node.Id, con.conID, resourceName, discReq.VersionInfo)
			}

			// In ingress gateway agent mode, if the first SDS request is received but kubernetes secret is not ready,
			// wait for secret before sending SDS response. If a kubernetes secret was deleted by operator, wait
			// for a new kubernetes secret before sending SDS response.
			if s.st.ShouldWaitForIngressGatewaySecret(con.conID, resourceName, token) {
				log.Debugf("Waiting for ingress gateway secret resource %q, connectionID %q, node %q\n", resourceName, con.conID, discReq.Node.Id)
				continue
			}

			secret, err := s.st.GenerateSecret(ctx, con.conID, resourceName, token)
			if err != nil {
				log.Errorf("Failed to get secret for proxy %q connection %q from secret cache: %v", discReq.Node.Id, con.conID, err)
				return err
			}
			con.secret = secret

			defer func() {
				removeConn(key)
				// Remove the secret from cache, otherwise refresh job will process this item(if envoy fails to reconnect)
				// and cause some confusing logs like 'fails to notify because connection isn't found'.
				s.st.DeleteSecret(con.conID, con.ResourceName)
			}()

			if err := pushSDS(con); err != nil {
				log.Errorf("SDS failed to push key/cert to proxy %q connection %q: %v", con.proxyID, con.conID, err)
				return err
			}
		case <-con.pushChannel:
			log.Debugf("Received push channel request for proxy %q connection %q", con.proxyID, con.conID)

			if con.secret == nil {
				// Secret is nil indicates close streaming to proxy, so that proxy
				// could connect again with updated token.
				// When nodeagent stops stream by sending envoy error response, it's Ok not to remove secret
				// from secret cache because cache has auto-evication.
				log.Debugf("Close streaming for proxy %q connection %q", con.proxyID, con.conID)
				return fmt.Errorf("streaming for proxy %q connection %q closed", con.proxyID, con.conID)
			}

			if err := pushSDS(con); err != nil {
				log.Errorf("SDS failed to push key/cert to proxy %q connection %q: %v", con.proxyID, con.conID, err)
				return err
			}
		}
	}
}

func (s *sdsservice) FetchSecrets(ctx context.Context, discReq *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	token := ""
	if !s.skipToken {
		t, err := getCredentialToken(ctx)
		if err != nil {
			log.Errorf("Failed to get credential token: %v", err)
			return nil, err
		}
		token = t
	}

	resourceName, err := parseDiscoveryRequest(discReq)
	if err != nil {
		log.Errorf("Failed to parse discovery request: %v", err)
		return nil, err
	}

	secret, err := s.st.GenerateSecret(ctx, discReq.Node.Id, resourceName, token)
	if err != nil {
		log.Errorf("Failed to get secret for proxy %q from secret cache: %v", discReq.Node.Id, err)
		return nil, err
	}
	return sdsDiscoveryResponse(secret, discReq.Node.Id)
}

// NotifyProxy send notification to proxy about secret update,
// SDS will close streaming connection if secret is nil.
func NotifyProxy(conID, resourceName string, secret *model.SecretItem) error {
	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: resourceName,
	}
	conn := sdsClients[key]
	if conn == nil {
		log.Errorf("No connection with id %q can be found", conID)
		return fmt.Errorf("no connection with id %q can be found", conID)
	}
	conn.mutex.Lock()
	conn.secret = secret
	conn.mutex.Unlock()

	conn.pushChannel <- &sdsEvent{}
	return nil
}

func parseDiscoveryRequest(discReq *xdsapi.DiscoveryRequest) (string /*resourceName*/, error) {
	if discReq.Node.Id == "" {
		return "", fmt.Errorf("discovery request %+v missing node id", discReq)
	}

	if len(discReq.ResourceNames) == 1 {
		return discReq.ResourceNames[0], nil
	}

	return "", fmt.Errorf("discovery request %+v has invalid resourceNames %+v", discReq, discReq.ResourceNames)
}

func getCredentialToken(ctx context.Context) (string, error) {
	metadata, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", fmt.Errorf("unable to get metadata from incoming context")
	}

	// Get credential token from request k8sSAJwtTokenHeader(`istio_sds_credentail_header`) if it exists;
	// otherwise fallback to credentialTokenHeader('authorization').
	if h, ok := metadata[k8sSAJwtTokenHeaderKey]; ok {
		if len(h) != 1 {
			return "", fmt.Errorf("credential token from %q must have 1 value in gRPC metadata but got %d", k8sSAJwtTokenHeaderKey, len(h))
		}
		return h[0], nil
	}

	if h, ok := metadata[credentialTokenHeaderKey]; ok {
		if len(h) != 1 {
			return "", fmt.Errorf("credential token from %q must have 1 value in gRPC metadata but got %d", credentialTokenHeaderKey, len(h))
		}
		return h[0], nil
	}

	return "", fmt.Errorf("no credential token is found")
}

func addConn(k cache.ConnKey, conn *sdsConnection) {
	sdsClientsMutex.Lock()
	defer sdsClientsMutex.Unlock()
	sdsClients[k] = conn
}

func removeConn(k cache.ConnKey) {
	sdsClientsMutex.Lock()
	defer sdsClientsMutex.Unlock()
	delete(sdsClients, k)
}

func pushSDS(con *sdsConnection) error {
	response, err := sdsDiscoveryResponse(con.secret, con.conID)
	if err != nil {
		log.Errorf("SDS: Failed to construct response %v", err)
		return err
	}

	if err = con.stream.Send(response); err != nil {
		log.Errorf("SDS: Send response failure %v", err)
		return err
	}

	con.mutex.RLock()
	if con.secret.RootCert != nil {
		log.Infof("SDS: push root cert from node agent to proxy connection: %q\n", con.conID)
		log.Debugf("SDS: push root cert %+v to proxy connection: %q\n", string(con.secret.RootCert), con.conID)
	} else {
		log.Infof("SDS: push key/cert pair from node agent to proxy: %q\n", con.conID)
		log.Debugf("SDS: push certificate chain %+v to proxy connection: %q\n", string(con.secret.CertificateChain), con.conID)
	}
	con.mutex.RUnlock()

	return nil
}

func sdsDiscoveryResponse(s *model.SecretItem, conID string) (*xdsapi.DiscoveryResponse, error) {
	resp := &xdsapi.DiscoveryResponse{
		TypeUrl:     SecretType,
		VersionInfo: s.Version,
		Nonce:       s.Version,
	}

	if s == nil {
		log.Errorf("SDS: got nil secret for proxy connection %q", conID)
		return resp, nil
	}

	secret := &authapi.Secret{
		Name: s.ResourceName,
	}
	if s.RootCert != nil {
		secret.Type = &authapi.Secret_ValidationContext{
			ValidationContext: &authapi.CertificateValidationContext{
				TrustedCa: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: s.RootCert,
					},
				},
			},
		}
	} else {
		secret.Type = &authapi.Secret_TlsCertificate{
			TlsCertificate: &authapi.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: s.CertificateChain,
					},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: s.PrivateKey,
					},
				},
			},
		}
	}

	ms, err := types.MarshalAny(secret)
	if err != nil {
		log.Errorf("Failed to mashal secret for proxy %q: %v", conID, err)
		return nil, err
	}
	resp.Resources = append(resp.Resources, *ms)

	return resp, nil
}

func newSDSConnection(stream discoveryStream) *sdsConnection {
	return &sdsConnection{
		pushChannel: make(chan *sdsEvent, 1),
		Connect:     time.Now(),
		stream:      stream,
	}
}

func receiveThread(con *sdsConnection, reqChannel chan *xdsapi.DiscoveryRequest, errP *error) {
	defer close(reqChannel) // indicates close of the remote side.
	for {
		req, err := con.stream.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled || err == io.EOF {
				log.Infof("SDS: connection with %q terminated %v", con.conID, err)
				return
			}
			*errP = err
			log.Errorf("SDS: connection with %q terminated with errors %v", con.conID, err)
			return
		}
		reqChannel <- req
	}
}

func constructConnectionID(proxyID string) string {
	id := atomic.AddInt64(&connectionNumber, 1)
	return proxyID + "-" + strconv.FormatInt(id, 10)
}
