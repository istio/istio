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
	"strings"
	"sync"
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
	"istio.io/istio/security/pkg/pki/util"
)

const (
	// SecretType is used for secret discovery service to construct response.
	SecretType = "type.googleapis.com/envoy.api.v2.auth.Secret"

	// CredentialTokenHeaderKey is the header key in gPRC header which is used to
	// pass credential token from envoy's SDS request to SDS service.
	CredentialTokenHeaderKey = "authorization"
)

var (
	sdsClients      = map[string]*sdsConnection{}
	sdsClientsMutex sync.RWMutex
)

type discoveryStream interface {
	Send(*xdsapi.DiscoveryResponse) error
	Recv() (*xdsapi.DiscoveryRequest, error)
	grpc.ServerStream
}

// sdsEvent represents a secret event that results in a push.
type sdsEvent struct {
	endStream bool
}

type sdsConnection struct {
	// Time of connection, for debugging.
	Connect time.Time

	// The ID of proxy from which the connection comes from.
	proxyID string

	// Sending on this channel results in  push.
	pushChannel chan *sdsEvent

	// SDS streams implement this interface.
	stream discoveryStream

	// The secret associated with the proxy.
	secret *model.SecretItem
}

type sdsservice struct {
	st cache.SecretManager
}

// newSDSService creates Secret Discovery Service which implements envoy v2 SDS API.
func newSDSService(st cache.SecretManager) *sdsservice {
	return &sdsservice{
		st: st,
	}
}

// register adds the SDS handle to the grpc server
func (s *sdsservice) register(rpcs *grpc.Server) {
	sds.RegisterSecretDiscoveryServiceServer(rpcs, s)
}

func (s *sdsservice) StreamSecrets(stream sds.SecretDiscoveryService_StreamSecretsServer) error {
	ctx := stream.Context()
	token, err := getCredentialToken(ctx)
	if err != nil {
		log.Errorf("Failed to get credential token from incoming request: %v", err)
		return err
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

			log.Debugf("Received discovery request from %q", discReq.Node.Id)

			con.proxyID = discReq.Node.Id
			spiffeID, err := parseDiscoveryRequest(discReq)
			if err != nil {
				log.Errorf("Failed to parse discovery request: %v", err)
				continue
			}

			// When nodeagent receives StreamSecrets request, if there is cached secret which matches
			// request's <token, resourceName(SpiffeID), Version>, then this request is a confirmation request.
			// nodeagent stops sending response to envoy in this case.
			if discReq.VersionInfo != "" && s.st.SecretExist(discReq.Node.Id, spiffeID, token, discReq.VersionInfo) {
				continue
			}

			secret, err := s.st.GenerateSecret(ctx, discReq.Node.Id, spiffeID, token)
			if err != nil {
				log.Errorf("Failed to get secret for proxy %q from secret cache: %v", discReq.Node.Id, err)
				return err
			}
			con.secret = secret

			addConn(discReq.Node.Id, con)
			defer removeConn(discReq.Node.Id)

			if err := pushSDS(con); err != nil {
				log.Errorf("SDS failed to push key/cert to proxy %q: %v", con.proxyID, err)
				return err
			}
		case <-con.pushChannel:
			log.Debugf("Received push channel request for %q", con.proxyID)

			if con.secret == nil {
				// Secret is nil indicates close streaming connection to proxy, so that proxy
				// could connect again with updated token.
				// When nodeagent stops stream by sending envoy error response, it's Ok not to remove secret
				// from secret cache because cache has auto-evication.
				log.Debugf("Close connection with %q", con.proxyID)
				return fmt.Errorf("streaming connection with %q closed", con.proxyID)
			}

			if err := pushSDS(con); err != nil {
				log.Errorf("SDS failed to push key/cert to proxy %q: %v", con.proxyID, err)
				return err
			}
		}
	}
}

func (s *sdsservice) FetchSecrets(ctx context.Context, discReq *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	token, err := getCredentialToken(ctx)
	if err != nil {
		log.Errorf("Failed to get credential token: %v", err)
		return nil, err
	}

	spiffeID, err := parseDiscoveryRequest(discReq)
	if err != nil {
		log.Errorf("Failed to parse discovery request: %v", err)
		return nil, err
	}

	secret, err := s.st.GenerateSecret(ctx, discReq.Node.Id, spiffeID, token)
	if err != nil {
		log.Errorf("Failed to get secret for proxy %q from secret cache: %v", discReq.Node.Id, err)
		return nil, err
	}

	return sdsDiscoveryResponse(secret, discReq.Node.Id)
}

// NotifyProxy send notification to proxy about secret update,
// SDS will close streaming connection is secret is nil.
func NotifyProxy(proxyID string, secret *model.SecretItem) error {
	conn := sdsClients[proxyID]
	if conn == nil {
		log.Errorf("No connection with id %q can be found", proxyID)
		return fmt.Errorf("no connection with id %q can be found", proxyID)
	}
	conn.secret = secret

	conn.pushChannel <- &sdsEvent{}
	return nil
}

func parseDiscoveryRequest(discReq *xdsapi.DiscoveryRequest) (string /*spiffeID*/, error) {
	if discReq.Node.Id == "" {
		return "", fmt.Errorf("discovery request %+v missing node id", discReq)
	}

	if len(discReq.ResourceNames) != 1 || !strings.HasPrefix(discReq.ResourceNames[0], util.URIScheme) {
		return "", fmt.Errorf("discovery request %+v has invalid resourceNames %+v", discReq, discReq.ResourceNames)
	}

	return discReq.ResourceNames[0], nil
}

func getCredentialToken(ctx context.Context) (string, error) {
	metadata, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", fmt.Errorf("unable to get metadata from incoming context")
	}

	if h, ok := metadata[CredentialTokenHeaderKey]; ok {
		if len(h) != 1 {
			return "", fmt.Errorf("credential token must have 1 value in gRPC metadata but got %d", len(h))
		}
		return h[0], nil
	}

	return "", fmt.Errorf("no credential token is found")
}

func addConn(proxyID string, conn *sdsConnection) {
	sdsClientsMutex.Lock()
	defer sdsClientsMutex.Unlock()
	sdsClients[proxyID] = conn
}

func removeConn(proxyID string) {
	sdsClientsMutex.Lock()
	defer sdsClientsMutex.Unlock()
	delete(sdsClients, proxyID)
}

func pushSDS(con *sdsConnection) error {
	log.Infof("SDS: push from node agent to proxy:%q", con.proxyID)

	response, err := sdsDiscoveryResponse(con.secret, con.proxyID)
	if err != nil {
		log.Errorf("SDS: Failed to construct response %v", err)
		return err
	}

	if err = con.stream.Send(response); err != nil {
		log.Errorf("SDS: Send response failure %v", err)
		return err
	}

	return nil
}

func sdsDiscoveryResponse(s *model.SecretItem, proxyID string) (*xdsapi.DiscoveryResponse, error) {
	resp := &xdsapi.DiscoveryResponse{
		TypeUrl:     SecretType,
		VersionInfo: s.Version,
		Nonce:       s.Version,
	}

	if s == nil {
		log.Errorf("SDS: got nil secret for proxy %q", proxyID)
		return resp, nil
	}

	secret := &authapi.Secret{
		Name: s.SpiffeID,
		Type: &authapi.Secret_TlsCertificate{
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
		},
	}

	ms, err := types.MarshalAny(secret)
	if err != nil {
		log.Errorf("Failed to mashal secret for proxy %q: %v", proxyID, err)
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
				log.Infof("SDS: connection with %q terminated %v", con.proxyID, err)
				return
			}
			*errP = err
			log.Errorf("SDS: connection with %q terminated with errors %v", con.proxyID, err)
			return
		}
		reqChannel <- req
	}
}
