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
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/pki/util"
)

const (
	// SecretType is used for secret discovery service to construct response.
	SecretType = "type.googleapis.com/envoy.api.v2.Secret"

	// credentialTokenHeaderKey is the header key in gPRC header which is used to
	// pass credential token from envoy to SDS.
	// TODO(quanlin): update value after confirming what headerKey that client side uses.
	credentialTokenHeaderKey = "access_token"
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
	// PeerAddr is the address of the client envoy, from network layer.
	PeerAddr string

	// Time of connection, for debugging.
	Connect time.Time

	// The proxy from which the connection comes from.
	proxy *model.Proxy

	// Sending on this channel results in  push.
	pushChannel chan *sdsEvent

	// doneChannel will be closed when the client is closed.
	doneChannel chan int

	// SDS streams implement this interface.
	stream discoveryStream

	// The secret associated with the proxy.
	secret *SecretItem
}

type sdsservice struct {
	st SecretManager
	//TODO(quanlin), add below properties later:
	//1. workloadRegistry(store proxies information).
}

// newSDSService creates Secret Discovery Service which implements envoy v2 SDS API.
func newSDSService(st SecretManager) *sdsservice {
	return &sdsservice{
		st: st,
	}
}

// register adds the SDS handle to the grpc server
func (s *sdsservice) register(rpcs *grpc.Server) {
	sds.RegisterSecretDiscoveryServiceServer(rpcs, s)
}

func (s *sdsservice) StreamSecrets(stream sds.SecretDiscoveryService_StreamSecretsServer) error {
	token, err := getCredentialToken(stream.Context())
	if err != nil {
		return err
	}

	peerAddr := "Unknown peer address"
	peerInfo, ok := peer.FromContext(stream.Context())
	if ok {
		peerAddr = peerInfo.Addr.String()
	}

	var discReq *xdsapi.DiscoveryRequest
	var receiveError error
	reqChannel := make(chan *xdsapi.DiscoveryRequest, 1)

	con := newSDSConnection(peerAddr, stream)
	defer close(con.doneChannel)

	go receiveThread(con, reqChannel, &receiveError)

	for {
		// Block until a request is received.
		select {
		case discReq, ok = <-reqChannel:
			if !ok {
				// Remote side closed connection.
				return receiveError
			}

			proxy, spiffeID, err := parseDiscoveryRequest(discReq)
			if err != nil {
				continue
			}
			con.proxy = proxy

			secret, err := s.st.GetSecret(discReq.Node.Id, spiffeID, token)
			if err != nil {
				log.Errorf("Failed to get secret for proxy %q from secret cache: %v", discReq.Node.Id, err)
				return err
			}
			con.secret = secret

			addConn(discReq.Node.Id, con)
			defer removeConn(discReq.Node.Id)

			if err := pushSDS(con); err != nil {
				log.Errorf("SDS failed to push: %v", err)
				return err
			}
		case <-con.pushChannel:
			if con.secret == nil {
				// Secret is nil indicates close streaming connection to proxy, so that proxy
				// could connect again with updated token.
				return fmt.Errorf("streaming connection closed")
			}

			if err := pushSDS(con); err != nil {
				log.Errorf("SDS failed to push: %v", err)
				return err
			}
		}
	}
}

func (s *sdsservice) FetchSecrets(ctx context.Context, discReq *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	token, err := getCredentialToken(ctx)
	if err != nil {
		return nil, err
	}

	proxy, spiffeID, err := parseDiscoveryRequest(discReq)
	if err != nil {
		return nil, err
	}

	secret, err := s.st.GetSecret(discReq.Node.Id, spiffeID, token)
	if err != nil {
		log.Errorf("Failed to get secret for proxy %q from secret cache: %v", discReq.Node.Id, err)
		return nil, err
	}

	return sdsDiscoveryResponse(secret, proxy)
}

// NotifyProxy send notification to proxy about secret update,
// SDS will close streaming connection is secret is nil.
func NotifyProxy(proxyID string, secret *SecretItem) error {
	cli := sdsClients[proxyID]
	if cli == nil {
		log.Infof("No sdsclient with id %q can be found", proxyID)
		return fmt.Errorf("no sdsclient with id %q can be found", proxyID)
	}
	cli.secret = secret

	cli.pushChannel <- &sdsEvent{}
	return nil
}

func parseDiscoveryRequest(discReq *xdsapi.DiscoveryRequest) (*model.Proxy, string, error) {
	if discReq.Node.Id == "" {
		return nil, "", fmt.Errorf("discovery request %+v missing node id", discReq)
	}

	if len(discReq.ResourceNames) != 1 || !strings.HasPrefix(discReq.ResourceNames[0], util.URIScheme) {
		return nil, "", fmt.Errorf("discovery request has invalid resourceNames %+v", discReq.ResourceNames)
	}
	spiffeID := discReq.ResourceNames[0]

	proxy, err := model.ParseServiceNode(discReq.Node.Id)
	if err != nil {
		log.Errorf("Failed to parse service node from discovery request %+v: %v", discReq, err)
		return nil, "", err
	}
	proxy.Metadata = model.ParseMetadata(discReq.Node.Metadata)

	return &proxy, spiffeID, nil
}

func getCredentialToken(ctx context.Context) (string, error) {
	metadata, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", fmt.Errorf("unable to get metadata from incoming context")
	}

	if h, ok := metadata[credentialTokenHeaderKey]; ok {
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
	response, err := sdsDiscoveryResponse(con.secret, con.proxy)
	if err != nil {
		log.Errorf("SDS: Failed to construct response %v", err)
		return err
	}

	if err = con.stream.Send(response); err != nil {
		log.Errorf("SDS: Send response failure %v", err)
		return err
	}

	log.Infof("SDS: push for proxy:%q addr:%q", con.proxy.ID, con.PeerAddr)
	return nil
}

func sdsDiscoveryResponse(s *SecretItem, proxy *model.Proxy) (*xdsapi.DiscoveryResponse, error) {
	//TODO(quanlin): use timestamp for versionInfo and nouce for now, may change later.
	t := time.Now().String()
	resp := &xdsapi.DiscoveryResponse{
		TypeUrl:     SecretType,
		VersionInfo: t,
		Nonce:       t,
	}

	if s == nil {
		log.Errorf("SDS: got nil secret for proxy %q", proxy.ID)
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
		log.Errorf("Failed to mashal secret for proxy %q: %v", proxy.ID, err)
		return nil, err
	}
	resp.Resources = append(resp.Resources, *ms)

	return resp, nil
}

func newSDSConnection(peerAddr string, stream discoveryStream) *sdsConnection {
	return &sdsConnection{
		doneChannel: make(chan int, 1),
		pushChannel: make(chan *sdsEvent, 1),
		PeerAddr:    peerAddr,
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
				log.Infof("SDS: %q terminated %v", con.PeerAddr, err)
				return
			}
			*errP = err
			log.Errorf("SDS: %q terminated with errors %v", con.PeerAddr, err)
			return
		}
		reqChannel <- req
	}
}
