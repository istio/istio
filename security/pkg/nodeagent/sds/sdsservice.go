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

// Package sds implements secret discovery service in NodeAgent.
package sds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv2 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/security"
	nodeagentutil "istio.io/istio/security/pkg/nodeagent/util"

	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/pkg/log"
)

const (
	// SecretType is used for secret discovery service to construct response.
	SecretTypeV2 = "type.googleapis.com/envoy.api.v2.auth.Secret"
	SecretTypeV3 = "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.Secret"

	// credentialTokenHeaderKey is the header key in gPRC header which is used to
	// pass credential token from envoy's SDS request to SDS service.
	credentialTokenHeaderKey = "authorization"

	// K8sSAJwtTokenHeaderKey is the request header key for k8s jwt token.
	// Binary header name must has suffix "-bin", according to https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md.
	// Same value defined in pilot pkg(k8sSAJwtTokenHeaderKey)
	k8sSAJwtTokenHeaderKey = "istio_sds_credentials_header-bin"
)

var (
	sdsClients       = map[cache.ConnKey]*sdsConnection{}
	staledClientKeys = map[cache.ConnKey]bool{}
	sdsClientsMutex  sync.RWMutex

	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)
	sdsServiceLog    = log.RegisterScope("sds", "SDS service debugging", 0)
)

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
	stream xds.DiscoveryStream

	// The secret associated with the proxy.
	secret *security.SecretItem

	// Mutex to protect read/write to this connection
	// TODO(JimmyCYJ): Move all read/write into member function with lock protection to avoid race condition.
	mutex sync.RWMutex

	// ConID is the connection identifier, used as a key in the connection table.
	// Currently based on the node name and a counter.
	conID string

	// Time of the recent SDS push. Will be reset to zero when a new SDS request is received. A
	// non-zero time indicates that the connection is waiting for SDS request.
	sdsPushTime time.Time

	// Envoy may request different versions of configuration (XDS v2 vs v3). While internally we will
	// only generate one version or the other, because the protos are wire compatible we can cast to the
	// requested version. This struct keeps track of the Secret type requested.
	// Note: while SDS is entirely local, Pilot determines the version during LDS, so we need to support both versions
	// In practice, if we are matched with a proper same version Pilot, we will always get v3.
	secretType string
}

type sdsservice struct {
	st security.SecretManager

	ticker         *time.Ticker
	tickerInterval time.Duration

	// close channel.
	closing chan bool

	// skipToken indicates whether token is required.
	skipToken bool

	fileMountedCertsOnly bool

	localJWT bool

	jwtPath string

	outputKeyCertToDir string

	// Credential fetcher
	credFetcher security.CredFetcher
}

// ClientDebug represents a single SDS connection to the ndoe agent
type ClientDebug struct {
	ConnectionID string `json:"connection_id"`
	ProxyID      string `json:"proxy"`
	ResourceName string `json:"resource_name"`

	// fields from secret item
	CertificateChain string `json:"certificate_chain"`
	RootCert         string `json:"root_cert"`
	CreatedTime      string `json:"created_time"`
	ExpireTime       string `json:"expire_time"`
}

// Debug represents all clients connected to this node agent endpoint and their supplied secrets
type Debug struct {
	Clients []ClientDebug `json:"clients"`
}

// newSDSService creates Secret Discovery Service which implements envoy v2 SDS API.
func newSDSService(st security.SecretManager,
	secOpt *security.Options,
	skipTokenVerification bool) *sdsservice {
	if st == nil {
		return nil
	}

	ret := &sdsservice{
		st:                   st,
		skipToken:            skipTokenVerification,
		fileMountedCertsOnly: secOpt.FileMountedCerts,
		tickerInterval:       secOpt.RecycleInterval,
		closing:              make(chan bool),
		localJWT:             secOpt.UseLocalJWT,
		jwtPath:              secOpt.JWTPath,
		outputKeyCertToDir:   secOpt.OutputKeyCertToDir,
		credFetcher:          secOpt.CredFetcher,
	}

	go ret.clearStaledClientsJob()

	return ret
}

// register adds the SDS handle to the grpc server
func (s *sdsservice) register(rpcs *grpc.Server) {
	sds.RegisterSecretDiscoveryServiceServer(rpcs, s)
	discoveryv2.RegisterSecretDiscoveryServiceServer(rpcs, s.createV2Adapter())
}

// DebugInfo serializes the current sds client data into JSON for the debug endpoint
func (s *sdsservice) DebugInfo() (string, error) {
	sdsClientsMutex.RLock()
	defer sdsClientsMutex.RUnlock()
	clientDebug := make([]ClientDebug, 0)
	for connKey, conn := range sdsClients {
		// it's possible for the connection to be established without an instantiated secret
		if conn.secret == nil {
			continue
		}

		conn.mutex.RLock()
		c := ClientDebug{
			ConnectionID:     connKey.ConnectionID,
			ProxyID:          conn.proxyID,
			ResourceName:     conn.ResourceName,
			CertificateChain: string(conn.secret.CertificateChain),
			RootCert:         string(conn.secret.RootCert),
			CreatedTime:      conn.secret.CreatedTime.Format(time.RFC3339),
			ExpireTime:       conn.secret.ExpireTime.Format(time.RFC3339),
		}
		clientDebug = append(clientDebug, c)
		conn.mutex.RUnlock()
	}

	debug := Debug{
		Clients: clientDebug,
	}
	debugJSON, err := json.MarshalIndent(debug, " ", "	")
	if err != nil {
		return "", err
	}

	return string(debugJSON), nil
}

func (s *sdsservice) DeltaSecrets(stream sds.SecretDiscoveryService_DeltaSecretsServer) error {
	return status.Error(codes.Unimplemented, "DeltaSecrets not implemented")
}

// StreamSecrets serves SDS discovery requests and SDS push requests
func (s *sdsservice) StreamSecrets(stream sds.SecretDiscoveryService_StreamSecretsServer) error {
	token := ""
	ctx := context.Background()

	var receiveError error
	reqChannel := make(chan *discovery.DiscoveryRequest, 1)
	con := newSDSConnection(stream)

	go receiveThread(con, reqChannel, &receiveError)

	for {
		// Block until a request is received.
		select {
		case discReq, ok := <-reqChannel:
			if !ok {
				// Remote side closed connection.
				sdsServiceLog.Errorf("Remote side closed connection")
				return receiveError
			}

			resourceName, err := getResourceName(discReq)
			if err != nil {
				sdsServiceLog.Errorf("Close connection. Error: %v", err)
				return err
			}

			if resourceName == "" {
				sdsServiceLog.Infof("Received empty resource name from %q. No need to respond", discReq.Node.Id)
				continue
			}
			con.secretType = discReq.TypeUrl

			var firstRequestFlag bool
			con.mutex.Lock()
			if con.conID == "" {
				// first request
				if discReq.Node == nil || len(discReq.Node.Id) == 0 {
					sdsServiceLog.Errorf("%s close connection. Missing Node ID in the first request",
						sdsLogPrefix(resourceName))
					return fmt.Errorf("missing Node ID in the first request")
				}
				con.conID = constructConnectionID(discReq.Node.Id)
				con.proxyID = discReq.Node.Id
				con.ResourceName = resourceName
				key := cache.ConnKey{
					ResourceName: resourceName,
					ConnectionID: con.conID,
				}
				addConn(key, con)
				firstRequestFlag = true
				sdsServiceLog.Infof("%s new connection", sdsLogPrefix(resourceName))
			}
			conID := con.conID

			// Reset SDS push time for new SDS push.
			con.sdsPushTime = time.Time{}
			con.mutex.Unlock()

			defer releaseResourcePerConn(s, conID, resourceName)

			conIDresourceNamePrefix := sdsLogPrefix(resourceName)
			if s.localJWT {
				// Running in-process, no need to pass the token from envoy to agent as in-context - use the file
				t, err := s.getToken()
				if err != nil {
					sdsServiceLog.Errorf("Failed to get credential token: %v", err)
					return err
				}
				token = t
			} else if s.outputKeyCertToDir != "" {
				// Using existing certs and the new SDS - skipToken case is for the old node agent.
			} else if !s.skipToken {
				ctx = stream.Context()
				t, err := getCredentialToken(ctx)
				if err != nil {
					sdsServiceLog.Errorf("%s Close connection. Failed to get credential token from "+
						"incoming request: %v", conIDresourceNamePrefix, err)
					return err
				}
				token = t
			}

			// Update metrics.
			totalActiveConnCounts.Increment()
			if discReq.ErrorDetail != nil {
				totalSecretUpdateFailureCounts.Increment()
				sdsServiceLog.Errorf("%s received error: %v. Will not respond until next secret update",
					conIDresourceNamePrefix, discReq.ErrorDetail)
				continue
			}
			// When nodeagent receives StreamSecrets request, if there is cached secret which matches
			// request's <token, resourceName, Version>, then this request is a confirmation request.
			// nodeagent stops sending response to envoy in this case.
			if discReq.VersionInfo != "" && s.st.SecretExist(conID, resourceName, token, discReq.VersionInfo) {
				sdsServiceLog.Debugf("%s received SDS ACK from proxy %q, version info %q, "+
					"error details %s\n", conIDresourceNamePrefix, discReq.Node.Id, discReq.VersionInfo,
					discReq.ErrorDetail)
				continue
			}

			sdsServiceLog.Debugf("%s received SDS request from proxy %q, first request: %v, version info %q, "+
				"error details %s\n", conIDresourceNamePrefix, discReq.Node.Id, firstRequestFlag, discReq.VersionInfo,
				discReq.ErrorDetail)

			// In gateway agent mode, if the first SDS request is received but gateway secret which is
			// provisioned as kubernetes secret is not ready, wait for secret before sending SDS response.
			// If a kubernetes secret was deleted by operator, wait for a new kubernetes secret before sending SDS response.
			// If workload uses file mounted certs i.e. "FILE_MOUNTED_CERTS" is set to true, workdload loads certificates from
			// mounted certificate paths and it does not depend on the presence of gateway secret so
			// we should skip waiting for it in that mode.
			// File mounted certs for gateways is used in scenarios where an existing PKI infrastuctures delivers certificates
			// to pods/VMs via files.
			if s.st.ShouldWaitForGatewaySecret(conID, resourceName, token, s.fileMountedCertsOnly) {
				sdsServiceLog.Warnf("%s waiting for gateway secret for proxy %q\n", conIDresourceNamePrefix, discReq.Node.Id)
				continue
			} else {
				sdsServiceLog.Infof("Skipping waiting for gateway secret")
			}

			secret, err := s.st.GenerateSecret(ctx, conID, resourceName, token)
			if err != nil {
				sdsServiceLog.Errorf("%s Close connection. Failed to get secret for proxy %q from "+
					"secret cache: %v", conIDresourceNamePrefix, discReq.Node.Id, err)
				return err
			}

			// Output the key and cert to a directory, if some applications need to read them from local file system.
			if err = nodeagentutil.OutputKeyCertToDir(s.outputKeyCertToDir, secret.PrivateKey,
				secret.CertificateChain, secret.RootCert); err != nil {
				sdsServiceLog.Errorf("(%v, %v) error when output the key and cert: %v",
					conIDresourceNamePrefix, discReq.Node.Id, err)
				return err
			}

			con.mutex.Lock()
			con.secret = secret
			con.mutex.Unlock()

			if err := pushSDS(con); err != nil {
				sdsServiceLog.Errorf("%s Close connection. Failed to push key/cert to proxy %q: %v",
					conIDresourceNamePrefix, discReq.Node.Id, err)
				return err
			}
		case <-con.pushChannel:
			con.mutex.RLock()
			proxyID := con.proxyID
			conID := con.conID
			resourceName := con.ResourceName
			secret := con.secret
			con.mutex.RUnlock()
			conIDresourceNamePrefix := sdsLogPrefix(resourceName)
			sdsServiceLog.Debugf("%s received push channel request for proxy %q", conIDresourceNamePrefix, proxyID)

			if secret == nil {
				defer releaseResourcePerConn(s, conID, resourceName)
				// Secret is nil indicates close streaming to proxy, so that proxy
				// could connect again with updated token.
				// When nodeagent stops stream by sending envoy error response, it's Ok not to remove secret
				// from secret cache because cache has auto-evication.
				sdsServiceLog.Debugf("%s close connection for proxy %q", conIDresourceNamePrefix, proxyID)
				return fmt.Errorf("%s Close connection to proxy %q", conIDresourceNamePrefix, conID)
			}

			if err := pushSDS(con); err != nil {
				sdsServiceLog.Errorf("%s Close connection. Failed to push key/cert to proxy %q: %v",
					conIDresourceNamePrefix, proxyID, err)
				return err
			}
			sdsServiceLog.Infoa("Dynamic push for secret ", resourceName)
		}
	}
}

// FetchSecrets generates and returns secret from SecretManager in response to DiscoveryRequest
func (s *sdsservice) FetchSecrets(ctx context.Context, discReq *discovery.DiscoveryRequest) (*discovery.DiscoveryResponse, error) {
	token := ""
	if s.localJWT {
		t, err := s.getToken()
		if err != nil {
			sdsServiceLog.Errorf("Failed to get credential token: %v", err)
			return nil, err
		}
		token = t
	} else if !s.skipToken {
		t, err := getCredentialToken(ctx)
		if err != nil {
			sdsServiceLog.Errorf("Failed to get credential token: %v", err)
			return nil, err
		}
		token = t
	}

	resourceName, err := getResourceName(discReq)
	if err != nil {
		sdsServiceLog.Errorf("Close connection. Error: %v", err)
		return nil, err
	}

	connID := constructConnectionID(discReq.Node.Id)
	secret, err := s.st.GenerateSecret(ctx, connID, resourceName, token)
	if err != nil {
		sdsServiceLog.Errorf("Failed to get secret for proxy %q from secret cache: %v", connID, err)
		return nil, err
	}

	// Output the key and cert to a directory, if some applications need to read them from local file system.
	if err = nodeagentutil.OutputKeyCertToDir(s.outputKeyCertToDir, secret.PrivateKey,
		secret.CertificateChain, secret.RootCert); err != nil {
		sdsServiceLog.Errorf("(%v) error when output the key and cert: %v",
			connID, err)
		return nil, err
	}
	return sdsDiscoveryResponse(secret, resourceName, discReq.TypeUrl)
}

func (s *sdsservice) getToken() (string, error) {
	token := ""
	if s.credFetcher != nil {
		t, err := s.credFetcher.GetPlatformCredential()
		if err != nil {
			sdsServiceLog.Errorf("Failed to get credential token through credential fetcher: %v", err)
			return "", err
		}
		token = t
	} else {
		tok, err := ioutil.ReadFile(s.jwtPath)
		if err != nil {
			sdsServiceLog.Errorf("failed to get credential token: %v from path %s", err, s.jwtPath)
			return "", err
		}
		token = string(tok)
	}
	return token, nil
}

func (s *sdsservice) Stop() {
	s.closing <- true
}

func (s *sdsservice) clearStaledClientsJob() {
	s.ticker = time.NewTicker(s.tickerInterval)
	for {
		select {
		case <-s.ticker.C:
			clearStaledClients()
		case <-s.closing:
			if s.ticker != nil {
				s.ticker.Stop()
			}
		}
	}
}

func clearStaledClients() {
	sdsServiceLog.Debug("start staled connection cleanup job")
	sdsClientsMutex.Lock()
	defer sdsClientsMutex.Unlock()

	for connKey := range staledClientKeys {
		sdsServiceLog.Debugf("remove staled clients %+v", connKey)
		delete(sdsClients, connKey)
		delete(staledClientKeys, connKey)
		// totalStaleConnCounts should be 0 when the for loop finishes.
		totalStaleConnCounts.Decrement()
	}
}

// NotifyProxy sends notification to proxy about secret update,
// SDS will close streaming connection if secret is nil.
func NotifyProxy(connKey cache.ConnKey, secret *security.SecretItem) error {
	conIDresourceNamePrefix := sdsLogPrefix(connKey.ResourceName)
	sdsClientsMutex.Lock()
	conn := sdsClients[connKey]
	if conn == nil {
		sdsClientsMutex.Unlock()
		sdsServiceLog.Errorf("%s NotifyProxy failed. No connection with id %q can be found",
			conIDresourceNamePrefix, connKey.ConnectionID)
		return fmt.Errorf("no connection with id %q can be found", connKey.ConnectionID)
	}
	conn.mutex.Lock()
	conn.secret = secret
	conn.mutex.Unlock()
	sdsClientsMutex.Unlock()

	conn.pushChannel <- &sdsEvent{}
	return nil
}

func releaseResourcePerConn(s *sdsservice, conID, resourceName string) {
	recycleConnection(conID, resourceName)
	// Remove the secret from cache, otherwise refresh job will process this item(if envoy fails to reconnect)
	// and cause some confusing logs like 'fails to notify because connection isn't found'.
	s.st.DeleteSecret(conID, resourceName)
}

func recycleConnection(conID, resourceName string) {
	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: resourceName,
	}

	sdsClientsMutex.Lock()
	defer sdsClientsMutex.Unlock()

	// Only add connection key to staledClientKeys if it's not there already.
	// The recycleConnection function may be triggered more than once for each connection key.
	// https://github.com/istio/istio/issues/15306#issuecomment-509783105
	if _, found := staledClientKeys[key]; found {
		return
	}

	staledClientKeys[key] = true

	totalStaleConnCounts.Increment()
	totalActiveConnCounts.Decrement()
}

func getResourceName(discReq *discovery.DiscoveryRequest) (string /*resourceName*/, error) {
	if len(discReq.ResourceNames) == 0 {
		return "", nil
	}

	if len(discReq.ResourceNames) == 1 {
		return discReq.ResourceNames[0], nil
	}

	return "", fmt.Errorf("discovery request %+v has more than one resourceNames %+v", discReq, discReq.ResourceNames)
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
		if len(h[0]) == 0 {
			return "", fmt.Errorf("received empty credential for existing header: %s", k8sSAJwtTokenHeaderKey)
		}
		return h[0], nil
	}

	if h, ok := metadata[credentialTokenHeaderKey]; ok {
		if len(h) != 1 {
			return "", fmt.Errorf("credential token from %q must have 1 value in gRPC metadata but got %d", credentialTokenHeaderKey, len(h))
		}
		if len(h[0]) == 0 {
			return "", fmt.Errorf("received empty credential for existing header: %s", credentialTokenHeaderKey)
		}
		return h[0], nil
	}

	return "", fmt.Errorf("no credential token is found")
}

func addConn(k cache.ConnKey, conn *sdsConnection) {
	sdsClientsMutex.Lock()
	defer sdsClientsMutex.Unlock()
	conIDresourceNamePrefix := sdsLogPrefix(k.ResourceName)
	sdsServiceLog.Debugf("%s add a new connection", conIDresourceNamePrefix)
	sdsClients[k] = conn
}

func pushSDS(con *sdsConnection) error {
	if con == nil {
		return fmt.Errorf("sdsConnection passed into pushSDS() should not be nil")
	}

	con.mutex.Lock()
	defer con.mutex.Unlock()
	secret := con.secret
	resourceName := con.ResourceName
	sdsPushTime := con.sdsPushTime

	conIDresourceNamePrefix := sdsLogPrefix(resourceName)
	if !sdsPushTime.IsZero() {
		sdsServiceLog.Errorf("%s skip multiple push, last push finishes at %s and is "+
			"waiting for next SDS request", conIDresourceNamePrefix, sdsPushTime.String())
		return nil
	}

	if secret == nil {
		return fmt.Errorf("sdsConnection %v passed into pushSDS() contains nil secret", con)
	}

	response, err := sdsDiscoveryResponse(secret, resourceName, con.secretType)
	if err != nil {
		sdsServiceLog.Errorf("%s failed to construct response for SDS push: %v", conIDresourceNamePrefix, err)
		return err
	}

	if err = con.stream.Send(response); err != nil {
		sdsServiceLog.Errorf("%s failed to send response: %v", conIDresourceNamePrefix, err)
		totalPushErrorCounts.Increment()
		return err
	}

	con.sdsPushTime = time.Now()

	// Update metrics after push to avoid adding latency to SDS push.
	if secret.RootCert != nil {
		sdsServiceLog.Infof("%s pushed root cert to proxy", conIDresourceNamePrefix)
		sdsServiceLog.Debugf("%s pushed root cert %+v to proxy", conIDresourceNamePrefix,
			string(secret.RootCert))
	} else {
		sdsServiceLog.Infof("%s pushed key/cert pair to proxy", conIDresourceNamePrefix)
		sdsServiceLog.Debugf("%s pushed certificate chain %+v to proxy",
			conIDresourceNamePrefix, string(secret.CertificateChain))
	}
	totalPushCounts.Increment()
	return nil
}

func sdsDiscoveryResponse(s *security.SecretItem, resourceName, typeURL string) (*discovery.DiscoveryResponse, error) {
	resp := &discovery.DiscoveryResponse{
		TypeUrl: typeURL,
	}
	conIDresourceNamePrefix := sdsLogPrefix(resourceName)
	if s == nil {
		sdsServiceLog.Warnf("%s got nil secret for proxy", conIDresourceNamePrefix)
		return resp, nil
	}

	resp.VersionInfo = s.Version
	resp.Nonce = s.Version
	secret := &tls.Secret{
		Name: s.ResourceName,
	}
	if s.RootCert != nil {
		secret.Type = &tls.Secret_ValidationContext{
			ValidationContext: &tls.CertificateValidationContext{
				TrustedCa: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: s.RootCert,
					},
				},
			},
		}
	} else {
		secret.Type = &tls.Secret_TlsCertificate{
			TlsCertificate: &tls.TlsCertificate{
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

	ms, err := ptypes.MarshalAny(secret)
	if err != nil {
		sdsServiceLog.Errorf("%s failed to mashal secret for proxy: %v", conIDresourceNamePrefix, err)
		return nil, err
	}
	ms.TypeUrl = typeURL
	resp.Resources = append(resp.Resources, ms)

	return resp, nil
}

func newSDSConnection(stream xds.DiscoveryStream) *sdsConnection {
	return &sdsConnection{
		pushChannel: make(chan *sdsEvent, 1),
		Connect:     time.Now(),
		stream:      stream,
	}
}

func receiveThread(con *sdsConnection, reqChannel chan *discovery.DiscoveryRequest, errP *error) {
	defer close(reqChannel) // indicates close of the remote side.
	for {
		req, err := con.stream.Recv()
		if err != nil {
			// Add read lock to avoid race condition with set con.conID in StreamSecrets.
			con.mutex.RLock()
			conIDresourceNamePrefix := sdsLogPrefix(con.ResourceName)
			con.mutex.RUnlock()
			if status.Code(err) == codes.Canceled || err == io.EOF {
				sdsServiceLog.Infof("%s connection is terminated: %v", conIDresourceNamePrefix, err)
				return
			}
			*errP = err
			sdsServiceLog.Errorf("%s connection is terminated with errors %v", conIDresourceNamePrefix, err)
			return
		}
		reqChannel <- req
	}
}

func constructConnectionID(proxyID string) string {
	id := atomic.AddInt64(&connectionNumber, 1)
	return proxyID + "-" + strconv.FormatInt(id, 10)
}

// sdsLogPrefix returns a unified log prefix.
func sdsLogPrefix(resourceName string) string {
	lPrefix := fmt.Sprintf("resource:%s", resourceName)
	return lPrefix
}
