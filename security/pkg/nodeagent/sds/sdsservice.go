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
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	cryptomb "github.com/envoyproxy/go-control-plane/contrib/envoy/extensions/private_key_providers/cryptomb/v3alpha"
	qat "github.com/envoyproxy/go-control-plane/contrib/envoy/extensions/private_key_providers/qat/v3alpha"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"github.com/google/uuid"
	uberatomic "go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/backoff"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/xds"
)

var sdsServiceLog = log.RegisterScope("sds", "SDS service debugging")

type sdsservice struct {
	st security.SecretManager

	stop       chan struct{}
	rootCaPath string
	pkpConf    *mesh.PrivateKeyProvider

	sync.Mutex
	clients map[string]*Context
}

type Context struct {
	BaseConnection xds.BaseConnection
	sync.Mutex
	s     *sdsservice
	watch *xds.WatchedResource
}

// newSDSService creates Secret Discovery Service which implements envoy SDS API.
func newSDSService(st security.SecretManager, options *security.Options, pkpConf *mesh.PrivateKeyProvider) *sdsservice {
	ret := &sdsservice{
		st:      st,
		stop:    make(chan struct{}),
		pkpConf: pkpConf,
		clients: make(map[string]*Context),
	}

	ret.rootCaPath = options.CARootPath

	if options.FileMountedCerts {
		return ret
	}

	// Pre-generate workload certificates to improve startup latency and ensure that for OUTPUT_CERTS
	// case we always write a certificate. A workload can technically run without any mTLS/CA
	// configured, in which case this will fail; if it becomes noisy we should disable the entire SDS
	// server in these cases.
	go func() {
		// TODO: do we need max timeout for retry, seems meaningless to retry forever if it never succeed
		b := backoff.NewExponentialBackOff(backoff.DefaultOption())
		// context for both timeout and channel, whichever stops first, the context will be done
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			select {
			case <-ret.stop:
				cancel()
			case <-ctx.Done():
			}
		}()
		defer cancel()
		_ = b.RetryWithContext(ctx, func() error {
			_, err := st.GenerateSecret(security.WorkloadKeyCertResourceName)
			if err != nil {
				sdsServiceLog.Warnf("failed to warm certificate: %v", err)
				return err
			}

			_, err = st.GenerateSecret(security.RootCertReqResourceName)
			if err != nil {
				sdsServiceLog.Warnf("failed to warm root certificate: %v", err)
				return err
			}

			return nil
		})
	}()

	return ret
}

func (s *sdsservice) generate(resourceNames []string) (xds.Resources, error) {
	resources := xds.Resources{}
	for _, resourceName := range resourceNames {
		secret, err := s.st.GenerateSecret(resourceName)
		if err != nil {
			// Typically, in Istiod, we do not return an error for a failure to generate a resource
			// However, here it makes sense, because we are generally streaming a single resource,
			// so sending an error will not cause a single failure to prevent the entire multiplex stream
			// of resources, and failures here are generally due to temporary networking issues to the CA
			// rather than a result of configuration issues, which trigger updates in Istiod when resolved.
			// Instead, we rely on the client to retry (with backoff) on failures.
			return nil, fmt.Errorf("failed to generate secret for %v: %v", resourceName, err)
		}

		res := protoconv.MessageToAny(toEnvoySecret(secret, s.rootCaPath, s.pkpConf))
		resources = append(resources, &discovery.Resource{
			Name:     resourceName,
			Resource: res,
		})
	}
	return resources, nil
}

// register adds the SDS handle to the grpc server
func (s *sdsservice) register(rpcs *grpc.Server) {
	sds.RegisterSecretDiscoveryServiceServer(rpcs, s)
}

func (s *sdsservice) push(name string) {
	s.Lock()
	defer s.Unlock()
	for _, client := range s.clients {
		go func() {
			select {
			case client.Connection().PushChannel <- name:
			case <-client.Connection().StreamClosed():
			}
		}()
	}
}

func (c Context) Connection() *xds.BaseConnection {
	return &c.BaseConnection
}

var connectionNumber = int64(0)

func (c *Context) Initialize(node *core.Node) error {
	id := atomic.AddInt64(&connectionNumber, 1)
	con := c.Connection()
	con.SetID(strconv.FormatInt(id, 10))

	c.s.Lock()
	c.s.clients[con.ID()] = c
	c.s.Unlock()

	con.DoneInitialize()
	return nil
}
func (c *Context) Close() {
	c.s.Lock()
	defer c.s.Unlock()
	delete(c.s.clients, c.Connection().ID())
}
func (c *Context) Watcher() xds.Watcher {
	return c
}
func (c *Context) DeleteWatchedResource(string) {
	c.Lock()
	defer c.Unlock()
	c.watch = nil
}
func (c *Context) GetWatchedResource(string) *xds.WatchedResource {
	c.Lock()
	defer c.Unlock()
	return c.watch
}
func (c *Context) NewWatchedResource(typeURL string, names []string) {
	c.Lock()
	defer c.Unlock()
	c.watch = &xds.WatchedResource{TypeUrl: typeURL, ResourceNames: names}
}
func (c *Context) UpdateWatchedResource(_ string, f func(*xds.WatchedResource) *xds.WatchedResource) {
	c.Lock()
	defer c.Unlock()
	c.watch = f(c.watch)
}
func (c *Context) GetID() string {
	return c.Connection().ID()
}

var version uberatomic.Uint64

func nextVersion() string {
	return time.Now().Format(time.RFC3339) + "/" + strconv.FormatUint(version.Inc(), 10)
}
func (c *Context) Process(req *discovery.DiscoveryRequest) error {
	shouldRespond, delta := xds.ShouldRespond(c.Watcher(), c.Connection().ID(), req)
	if !shouldRespond {
		return nil
	}
	resources := req.ResourceNames
	if !delta.IsEmpty() {
		resources = delta.Subscribed.UnsortedList()
	}
	res, err := c.s.generate(resources)
	if err != nil {
		return err
	}
	resp := &discovery.DiscoveryResponse{
		TypeUrl:     model.SecretType,
		VersionInfo: nextVersion(),
		Nonce:       uuid.New().String(),
		Resources:   xds.ResourcesToAny(res),
	}
	return xds.Send(c, resp)
}
func (c *Context) requested(name string) bool {
	c.Lock()
	defer c.Unlock()
	if c.watch != nil {
		for _, res := range c.watch.ResourceNames {
			if res == name {
				return true
			}
		}
	}
	return false
}
func (c *Context) Push(ev any) error {
	name := ev.(string)
	if !c.requested(name) {
		return nil
	}
	res, err := c.s.generate([]string{name})
	if err != nil {
		return err
	}
	return xds.Send(c, &discovery.DiscoveryResponse{
		TypeUrl:     model.SecretType,
		VersionInfo: nextVersion(),
		Nonce:       uuid.New().String(),
		Resources:   xds.ResourcesToAny(res),
	})
}

// StreamSecrets serves SDS discovery requests and SDS push requests
func (s *sdsservice) StreamSecrets(stream sds.SecretDiscoveryService_StreamSecretsServer) error {
	return xds.Stream(&Context{
		BaseConnection: xds.NewConnection("", stream),
		s:              s,
	})
}

func (s *sdsservice) DeltaSecrets(stream sds.SecretDiscoveryService_DeltaSecretsServer) error {
	return status.Error(codes.Unimplemented, "DeltaSecrets not implemented")
}

func (s *sdsservice) FetchSecrets(ctx context.Context, discReq *discovery.DiscoveryRequest) (*discovery.DiscoveryResponse, error) {
	return nil, status.Error(codes.Unimplemented, "FetchSecrets not implemented")
}

func (s *sdsservice) Close() {
	close(s.stop)
}

// toEnvoySecret converts a security.SecretItem to an Envoy tls.Secret
func toEnvoySecret(s *security.SecretItem, caRootPath string, pkpConf *mesh.PrivateKeyProvider) *tls.Secret {
	secret := &tls.Secret{
		Name: s.ResourceName,
	}
	var cfg security.SdsCertificateConfig
	ok := false
	if s.ResourceName == security.FileRootSystemCACert {
		cfg, ok = security.SdsCertificateConfigFromResourceNameForOSCACert(caRootPath)
	} else {
		cfg, ok = security.SdsCertificateConfigFromResourceName(s.ResourceName)
	}
	if s.ResourceName == security.RootCertReqResourceName || (ok && cfg.IsRootCertificate()) {
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
		switch pkpConf.GetProvider().(type) {
		case *mesh.PrivateKeyProvider_Cryptomb:
			crypto := pkpConf.GetCryptomb()
			msg := protoconv.MessageToAny(&cryptomb.CryptoMbPrivateKeyMethodConfig{
				PollDelay: durationpb.New(time.Duration(crypto.GetPollDelay().Nanos)),
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: s.PrivateKey,
					},
				},
			})
			secret.Type = &tls.Secret_TlsCertificate{
				TlsCertificate: &tls.TlsCertificate{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{
							InlineBytes: s.CertificateChain,
						},
					},
					PrivateKeyProvider: &tls.PrivateKeyProvider{
						ProviderName: "cryptomb",
						ConfigType: &tls.PrivateKeyProvider_TypedConfig{
							TypedConfig: msg,
						},
						Fallback: crypto.GetFallback().GetValue(),
					},
				},
			}
		case *mesh.PrivateKeyProvider_Qat:
			qatConf := pkpConf.GetQat()
			msg := protoconv.MessageToAny(&qat.QatPrivateKeyMethodConfig{
				PollDelay: durationpb.New(time.Duration(qatConf.GetPollDelay().Nanos)),
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: s.PrivateKey,
					},
				},
			})
			secret.Type = &tls.Secret_TlsCertificate{
				TlsCertificate: &tls.TlsCertificate{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{
							InlineBytes: s.CertificateChain,
						},
					},
					PrivateKeyProvider: &tls.PrivateKeyProvider{
						ProviderName: "qat",
						ConfigType: &tls.PrivateKeyProvider_TypedConfig{
							TypedConfig: msg,
						},
						Fallback: qatConf.GetFallback().GetValue(),
					},
				},
			}
		default:
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
	}
	return secret
}
