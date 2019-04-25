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
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/plugin"
	"istio.io/istio/security/pkg/nodeagent/plugin/providers/google/stsclient"
)

const maxStreams = 100000
const maxRetryTimes = 5

// Options provides all of the configuration parameters for secret discovery service.
type Options struct {
	// WorkloadUDSPath is the unix domain socket through which SDS server communicates with workload proxies.
	WorkloadUDSPath string

	// IngressGatewayUDSPath is the unix domain socket through which SDS server communicates with
	// ingress gateway proxies.
	IngressGatewayUDSPath string

	// CertFile is the path of Cert File for gRPC server TLS settings.
	CertFile string

	// KeyFile is the path of Key File for gRPC server TLS settings.
	KeyFile string

	// CAEndpoint is the CA endpoint to which node agent sends CSR request.
	CAEndpoint string

	// The CA provider name.
	CAProviderName string

	// TrustDomain corresponds to the trust root of a system.
	// https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain
	TrustDomain string

	// PluginNames is plugins' name for certain authentication provider.
	PluginNames []string

	// The Vault CA address.
	VaultAddress string

	// The Vault auth path.
	VaultAuthPath string

	// The Vault role.
	VaultRole string

	// The Vault sign CSR path.
	VaultSignCsrPath string

	// The Vault TLS root certificate.
	VaultTLSRootCert string

	// EnableWorkloadSDS indicates whether node agent works as SDS server for workload proxies.
	EnableWorkloadSDS bool

	// EnableIngressGatewaySDS indicates whether node agent works as ingress gateway agent.
	EnableIngressGatewaySDS bool
	// AlwaysValidTokenFlag is set to true for if token used is always valid(ex, normal k8s JWT)
	AlwaysValidTokenFlag bool
}

// Server is the gPRC server that exposes SDS through UDS.
type Server struct {
	workloadSds *sdsservice
	gatewaySds  *sdsservice

	grpcWorkloadListener net.Listener
	grpcGatewayListener  net.Listener

	grpcWorkloadServer *grpc.Server
	grpcGatewayServer  *grpc.Server
}

// NewServer creates and starts the Grpc server for SDS.
func NewServer(options Options, workloadSecretCache, gatewaySecretCache cache.SecretManager) (*Server, error) {
	s := &Server{
		workloadSds: newSDSService(workloadSecretCache, false),
		gatewaySds:  newSDSService(gatewaySecretCache, true),
	}
	if options.EnableWorkloadSDS {
		if err := s.initWorkloadSdsService(&options); err != nil {
			log.Errorf("Failed to initialize secret discovery service for workload proxies: %v", err)
			return nil, err
		}
		log.Infof("SDS gRPC server for workload UDS starts, listening on %q \n", options.WorkloadUDSPath)
	}

	if options.EnableIngressGatewaySDS {
		if err := s.initGatewaySdsService(&options); err != nil {
			log.Errorf("Failed to initialize secret discovery service for ingress gateway: %v", err)
			return nil, err
		}
		log.Infof("SDS gRPC server for ingress gateway controller starts, listening on %q \n",
			options.IngressGatewayUDSPath)
	}

	return s, nil
}

// Stop closes the gRPC server.
func (s *Server) Stop() {
	if s == nil {
		return
	}

	if s.grpcWorkloadListener != nil {
		s.grpcWorkloadListener.Close()
	}
	if s.grpcGatewayListener != nil {
		s.grpcGatewayListener.Close()
	}

	if s.grpcWorkloadServer != nil {
		s.grpcWorkloadServer.Stop()
	}
	if s.grpcGatewayServer != nil {
		s.grpcGatewayServer.Stop()
	}
}

// NewPlugins returns a slice of default Plugins.
func NewPlugins(in []string) []plugin.Plugin {
	var availablePlugins = map[string]plugin.Plugin{
		plugin.GoogleTokenExchange: stsclient.NewPlugin(),
	}
	var plugins []plugin.Plugin
	for _, pl := range in {
		if p, exist := availablePlugins[pl]; exist {
			plugins = append(plugins, p)
		}
	}
	return plugins
}

func (s *Server) initWorkloadSdsService(options *Options) error { //nolint: unparam
	s.grpcWorkloadServer = grpc.NewServer(s.grpcServerOptions(options)...)
	s.workloadSds.register(s.grpcWorkloadServer)

	var err error
	s.grpcWorkloadListener, err = setUpUds(options.WorkloadUDSPath)
	if err != nil {
		log.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
	}

	go func() {
		log.Info("Start SDS grpc server")
		waitTime := time.Second
		for i := 0; i < maxRetryTimes; i++ {
			// Retry if Serve() fails
			if err = s.grpcWorkloadServer.Serve(s.grpcWorkloadListener); err != nil {
				log.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
			}
			s.grpcWorkloadListener, err = setUpUds(options.WorkloadUDSPath)
			if err != nil {
				log.Errorf("SDS grpc server for workload proxies failed to set up UDS: %v", err)
			}
			time.Sleep(waitTime)
			waitTime *= 2
		}
	}()

	return nil
}

func (s *Server) initGatewaySdsService(options *Options) error {
	s.grpcGatewayServer = grpc.NewServer(s.grpcServerOptions(options)...)
	s.gatewaySds.register(s.grpcGatewayServer)

	var err error
	s.grpcGatewayListener, err = setUpUds(options.IngressGatewayUDSPath)
	if err != nil {
		log.Errorf("SDS grpc server for ingress gateway proxy failed to start: %v", err)
		return fmt.Errorf("SDS grpc server for ingress gateway proxy failed to start: %v", err)
	}

	go func() {
		log.Info("Start SDS grpc server for ingress gateway proxy")
		waitTime := time.Second
		for i := 0; i < maxRetryTimes; i++ {
			// Retry if Serve() fails
			if err = s.grpcGatewayServer.Serve(s.grpcGatewayListener); err != nil {
				log.Errorf("SDS grpc server for ingress gateway proxy failed to start: %v", err)
			}
			s.grpcGatewayListener, err = setUpUds(options.IngressGatewayUDSPath)
			if err != nil {
				log.Errorf("SDS grpc server for ingress gateway proxy failed to set up UDS: %v", err)
			}
			time.Sleep(waitTime)
			waitTime *= 2
		}
	}()

	return nil
}

func setUpUds(udsPath string) (net.Listener, error) {
	// Remove unix socket before use.
	if err := os.Remove(udsPath); err != nil && !os.IsNotExist(err) {
		// Anything other than "file not found" is an error.
		log.Errorf("Failed to remove unix://%s: %v", udsPath, err)
		return nil, fmt.Errorf("failed to remove unix://%s", udsPath)
	}

	var err error
	udsListener, err := net.Listen("unix", udsPath)
	if err != nil {
		log.Errorf("Failed to listen on unix socket %q: %v", udsPath, err)
		return nil, err
	}

	// Update SDS UDS file permission so that istio-proxy has permission to access it.
	if _, err := os.Stat(udsPath); err != nil {
		log.Errorf("SDS uds file %q doesn't exist", udsPath)
		return nil, fmt.Errorf("sds uds file %q doesn't exist", udsPath)
	}
	if err := os.Chmod(udsPath, 0666); err != nil {
		log.Errorf("Failed to update %q permission", udsPath)
		return nil, fmt.Errorf("failed to update %q permission", udsPath)
	}

	return udsListener, nil
}

func (s *Server) grpcServerOptions(options *Options) []grpc.ServerOption {
	grpcOptions := []grpc.ServerOption{
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
	}

	if options.CertFile != "" && options.KeyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(options.CertFile, options.KeyFile)
		if err != nil {
			log.Errorf("Failed to load TLS keys: %s", err)
			return nil
		}
		grpcOptions = append(grpcOptions, grpc.Creds(creds))
	}

	return grpcOptions
}
