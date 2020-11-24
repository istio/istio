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

package sds

import (
	"context"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	ca2 "istio.io/istio/pkg/security"
	"istio.io/istio/pkg/uds"
	"istio.io/istio/security/pkg/nodeagent/plugin"
	"istio.io/istio/security/pkg/nodeagent/plugin/providers/google/stsclient"
	"istio.io/pkg/version"
)

const (
	// base HTTP route for debug endpoints
	maxStreams    = 100000
	maxRetryTimes = 5
)

// Server is the gPRC server that exposes SDS through UDS.
type Server struct {
	workloadSds *sdsservice

	grpcWorkloadListener net.Listener

	grpcWorkloadServer *grpc.Server
	debugServer        *http.Server
}

// NewServer creates and starts the Grpc server for SDS.
func NewServer(options *ca2.Options, workloadSecretCache ca2.SecretManager) (*Server, error) {
	s := &Server{
		workloadSds: newSDSService(workloadSecretCache, options, options.FileMountedCerts),
	}
	if options.EnableWorkloadSDS {
		if err := s.initWorkloadSdsService(options); err != nil {
			sdsServiceLog.Errorf("Failed to initialize secret discovery service for workload proxies: %v", err)
			return nil, err
		}
		sdsServiceLog.Infof("SDS gRPC server for workload UDS starts, listening on %q", options.WorkloadUDSPath)
	}

	version.Info.RecordComponentBuildTag("citadel_agent")

	return s, nil
}

// Stop closes the gRPC server and debug server.
func (s *Server) Stop() {
	if s == nil {
		return
	}

	if s.grpcWorkloadListener != nil {
		s.grpcWorkloadListener.Close()
	}
	if s.grpcWorkloadServer != nil {
		s.workloadSds.Stop()
		s.grpcWorkloadServer.Stop()
	}

	if s.debugServer != nil {
		if err := s.debugServer.Shutdown(context.TODO()); err != nil {
			sdsServiceLog.Error("failed to shut down debug server")
		}
	}
}

// NewPlugins returns a slice of default Plugins.
func NewPlugins(in []string) []ca2.TokenExchanger {
	var availablePlugins = map[string]ca2.TokenExchanger{
		plugin.GoogleTokenExchange: stsclient.NewPlugin(),
	}
	var plugins []ca2.TokenExchanger
	for _, pl := range in {
		if p, exist := availablePlugins[pl]; exist {
			plugins = append(plugins, p)
		}
	}
	return plugins
}

func (s *Server) initWorkloadSdsService(options *ca2.Options) error { //nolint: unparam
	if options.GrpcServer != nil {
		s.grpcWorkloadServer = options.GrpcServer
		s.workloadSds.register(s.grpcWorkloadServer)
		return nil
	}
	s.grpcWorkloadServer = grpc.NewServer(s.grpcServerOptions(options)...)
	s.workloadSds.register(s.grpcWorkloadServer)

	var err error
	s.grpcWorkloadListener, err = uds.NewListener(options.WorkloadUDSPath)
	if err != nil {
		sdsServiceLog.Errorf("Failed to set up UDS path: %v", err)
	}

	go func() {
		sdsServiceLog.Info("Start SDS grpc server")
		waitTime := time.Second

		for i := 0; i < maxRetryTimes; i++ {
			serverOk := true
			setUpUdsOK := true
			if s.grpcWorkloadListener != nil {
				if err = s.grpcWorkloadServer.Serve(s.grpcWorkloadListener); err != nil {
					sdsServiceLog.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
					serverOk = false
				}
			}
			if s.grpcWorkloadListener == nil {
				if s.grpcWorkloadListener, err = uds.NewListener(options.WorkloadUDSPath); err != nil {
					sdsServiceLog.Errorf("SDS grpc server for workload proxies failed to set up UDS: %v", err)
					setUpUdsOK = false
				}
			}
			if serverOk && setUpUdsOK {
				break
			}
			time.Sleep(waitTime)
			waitTime *= 2
		}
	}()

	return nil
}

func (s *Server) grpcServerOptions(options *ca2.Options) []grpc.ServerOption {
	grpcOptions := []grpc.ServerOption{
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
	}

	if options.CertFile != "" && options.KeyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(options.CertFile, options.KeyFile)
		if err != nil {
			sdsServiceLog.Errorf("Failed to load TLS keys: %s", err)
			return nil
		}
		grpcOptions = append(grpcOptions, grpc.Creds(creds))
	}

	return grpcOptions
}
