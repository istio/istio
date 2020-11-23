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
	"fmt"
	"io/ioutil"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	xdscache "istio.io/istio/pkg/config/xds/cache"
	"istio.io/istio/pkg/security"
	nodeagentutil "istio.io/istio/security/pkg/nodeagent/util"
	"istio.io/pkg/log"
)

const (
	// SecretType is used for secret discovery service to construct response.
	SecretTypeV3 = "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.Secret"
)

var (
	sdsServiceLog = log.RegisterScope("sds", "SDS service debugging", 0)
)

type sdsservice struct {
	st security.SecretManager

	// skipToken indicates whether token is required.
	skipToken bool

	localJWT bool

	jwtPath string

	outputKeyCertToDir string

	// Credential fetcher
	credFetcher security.CredFetcher
	XdsServer   *XdsServer
}

type XdsServer struct {
	Server   serverv3.Server
	Cache    *xdscache.LinearCache
	Generate func(resourceName string) (proto.Message, error)
}

func (x XdsServer) OnFetchRequest(ctx context.Context, request *discovery.DiscoveryRequest) error {
	return nil
}

func (x XdsServer) OnFetchResponse(request *discovery.DiscoveryRequest, response *discovery.DiscoveryResponse) {
}

func (x XdsServer) OnStreamOpen(ctx context.Context, i int64, s string) error {
	log.Errorf("howardjohn: stream open %v", s)
	return nil
}

func (x XdsServer) OnStreamClosed(i int64) {
	log.Errorf("howardjohn: stream closed")
}

func (x *XdsServer) GenerateResources(rn []string) error {
	for _, rn := range rn {
		secret, err := x.Generate(rn)
		if err != nil {
			return err
		}
		if err := x.Cache.UpdateResource(rn, secret, false); err != nil {
			return err
		}
	}
	return nil
}

func (x *XdsServer) Set(rn string, item *security.SecretItem) error {
	log.Errorf("howardjohn: set %v", rn)
	return x.Cache.UpdateResource(rn, toEnvoySecret(item), true)
}

func (x *XdsServer) OnStreamRequest(i int64, request *discovery.DiscoveryRequest) error {
	log.Errorf("howardjohn: request %v/%v", request.TypeUrl, request.ResourceNames)
	for _, rn := range request.ResourceNames {
		if !x.Cache.Contains(rn) {
			if err := x.GenerateResources([]string{rn}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (x XdsServer) OnStreamResponse(i int64, request *discovery.DiscoveryRequest, response *discovery.DiscoveryResponse) {
	log.Errorf("howardjohn: response %+v for %v", len(response.Resources), request.ResourceNames)
}

func NewXdsServer(generate func(resourceName string) (proto.Message, error)) *XdsServer {
	out := &XdsServer{Generate: generate}

	out.Cache = xdscache.NewLinearCache(v3.SecretType)
	out.Server = serverv3.NewServer(context.Background(), out.Cache, out)
	return out
}

// newSDSService creates Secret Discovery Service which implements envoy SDS API.
func newSDSService(st security.SecretManager,
	secOpt *security.Options,
	skipTokenVerification bool) *sdsservice {
	if st == nil {
		return nil
	}

	ret := &sdsservice{
		st:                 st,
		skipToken:          skipTokenVerification,
		localJWT:           secOpt.UseLocalJWT,
		jwtPath:            secOpt.JWTPath,
		outputKeyCertToDir: secOpt.OutputKeyCertToDir,
		credFetcher:        secOpt.CredFetcher,
	}
	ret.XdsServer = NewXdsServer(ret.generateSingle)

	// Pre-generate workload certificates to improve startup latency
	_ = ret.XdsServer.GenerateResources([]string{"default", "ROOTCA"})

	return ret
}

func (s *sdsservice) fetchToken() (string, error) {
	if s.localJWT {
		// Running in-process, no need to pass the token from envoy to agent as in-context - use the file
		t, err := s.getToken()
		if err != nil {
			return "", fmt.Errorf("failed to get credential token: %v", err)
		}
		return t, nil
	}
	return "", nil
}

func (s *sdsservice) generateSingle(resourceName string) (proto.Message, error) {
	ctx := context.Background()
	token, err := s.fetchToken()
	if err != nil {
		sdsServiceLog.Errorf("failed to fetch token: %v", err)
		return nil, err
	}
	secret, err := s.st.GenerateSecret(ctx, "fake", resourceName, token)
	if err != nil {
		sdsServiceLog.Errorf("failed to generate secret for %v: %v", resourceName, err)
		return nil, err
	}
	log.Errorf("howardjohn: generated %v: %v", resourceName, secret.CreatedTime)
	return toEnvoySecret(secret), nil
}

func (s *sdsservice) generate(resourceNames []string) model.Resources {
	ctx := context.Background()
	token, err := s.fetchToken()
	if err != nil {
		sdsServiceLog.Errorf("failed to fetch token: %v", err)
		return nil
	}
	resources := model.Resources{}
	for _, resourceName := range resourceNames {
		secret, err := s.st.GenerateSecret(ctx, "fake", resourceName, token)
		if err != nil {
			sdsServiceLog.Errorf("failed to generate secret for %v: %v", resourceName, err)
			continue
		}
		if err = nodeagentutil.OutputKeyCertToDir(s.outputKeyCertToDir, secret.PrivateKey,
			secret.CertificateChain, secret.RootCert); err != nil {
			sdsServiceLog.Errorf("error when output the key and cert: %v", err)
		}

		resources = append(resources, util.MessageToAny(toEnvoySecret(secret)))
	}
	return resources
}

func (s *sdsservice) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates *model.PushRequest) model.Resources {
	return s.generate(w.ResourceNames)
}

// register adds the SDS handle to the grpc server
func (s *sdsservice) register(rpcs *grpc.Server) {
	sds.RegisterSecretDiscoveryServiceServer(rpcs, s)
}

func (s *sdsservice) DeltaSecrets(stream sds.SecretDiscoveryService_DeltaSecretsServer) error {
	return status.Error(codes.Unimplemented, "DeltaSecrets not implemented")
}

// StreamSecrets serves SDS discovery requests and SDS push requests
func (s *sdsservice) StreamSecrets(stream sds.SecretDiscoveryService_StreamSecretsServer) error {
	return s.XdsServer.Server.StreamSecrets(stream)
}

// FetchSecrets generates and returns secret from SecretManager in response to DiscoveryRequest
func (s *sdsservice) FetchSecrets(ctx context.Context, discReq *discovery.DiscoveryRequest) (*discovery.DiscoveryResponse, error) {
	return s.XdsServer.Server.FetchSecrets(ctx, discReq)
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

func toEnvoySecret(s *security.SecretItem) *tls.Secret {
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

	return secret
}
