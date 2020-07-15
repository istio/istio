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
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	ca2 "istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/plugin"
	"istio.io/istio/security/pkg/nodeagent/plugin/providers/google/stsclient"
	"istio.io/pkg/version"
)

const (
	// base HTTP route for debug endpoints
	debugBase     = "/debug"
	maxStreams    = 100000
	maxRetryTimes = 5
)

// Server is the gPRC server that exposes SDS through UDS.
type Server struct {
	workloadSds *sdsservice
	gatewaySds  *sdsservice

	grpcWorkloadListener net.Listener
	grpcGatewayListener  net.Listener

	grpcWorkloadServer *grpc.Server
	grpcGatewayServer  *grpc.Server
	debugServer        *http.Server
}

// NewServer creates and starts the Grpc server for SDS.
func NewServer(options *ca2.Options, workloadSecretCache, gatewaySecretCache ca2.SecretManager) (*Server, error) {
	s := &Server{
		workloadSds: newSDSService(workloadSecretCache,
			options,
			options.FileMountedCerts),
		gatewaySds: newSDSService(gatewaySecretCache, options,
			true),
	}
	if options.EnableWorkloadSDS {
		if err := s.initWorkloadSdsService(options); err != nil {
			sdsServiceLog.Errorf("Failed to initialize secret discovery service for workload proxies: %v", err)
			return nil, err
		}
		sdsServiceLog.Infof("SDS gRPC server for workload UDS starts, listening on %q \n", options.WorkloadUDSPath)
	}

	if options.EnableGatewaySDS {
		if err := s.initGatewaySdsService(options); err != nil {
			sdsServiceLog.Errorf("Failed to initialize secret discovery service for gateway: %v", err)
			return nil, err
		}
		sdsServiceLog.Infof("SDS gRPC server for gateway controller starts, listening on %q \n",
			options.GatewayUDSPath)
	}

	version.Info.RecordComponentBuildTag("citadel_agent")

	if options.DebugPort > 0 {
		s.initDebugServer(options.DebugPort)
	}
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
	if s.grpcGatewayListener != nil {
		s.grpcGatewayListener.Close()
	}

	if s.grpcWorkloadServer != nil {
		s.workloadSds.Stop()
		s.grpcWorkloadServer.Stop()
	}
	if s.grpcGatewayServer != nil {
		s.gatewaySds.Stop()
		s.grpcGatewayServer.Stop()
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

func (s *Server) initDebugServer(port int) {
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("%s/sds/workload", debugBase), s.workloadSds.debugHTTPHandler)
	mux.HandleFunc(fmt.Sprintf("%s/sds/gateway", debugBase), s.gatewaySds.debugHTTPHandler)
	s.debugServer = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}

	go func() {
		err := s.debugServer.ListenAndServe()
		sdsServiceLog.Errorf("debug server failure: %s", err)
	}()
}

func (s *sdsservice) debugHTTPHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Content-Type", "application/json")

	workloadJSON, err := s.DebugInfo()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		failureMessage := fmt.Sprintf("debug endpoint failure: %s", err)
		if _, err := w.Write([]byte(failureMessage)); err != nil {
			sdsServiceLog.Errorf("debug endpoint failed to write error response: %s", err)
		}
		return
	}
	if _, err := w.Write([]byte(workloadJSON)); err != nil {
		sdsServiceLog.Errorf("debug endpoint failed to write response: %s", err)
	}
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
	s.grpcWorkloadListener, err = setUpUds(options.WorkloadUDSPath)
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
				if s.grpcWorkloadListener, err = setUpUds(options.WorkloadUDSPath); err != nil {
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

func (s *Server) initGatewaySdsService(options *ca2.Options) error {
	s.grpcGatewayServer = grpc.NewServer(s.grpcServerOptions(options)...)
	s.gatewaySds.register(s.grpcGatewayServer)

	var err error
	s.grpcGatewayListener, err = setUpUds(options.GatewayUDSPath)
	if err != nil {
		sdsServiceLog.Errorf("SDS grpc server for ingress gateway proxy failed to start: %v", err)
		return fmt.Errorf("SDS grpc server for ingress gateway proxy failed to start: %v", err)
	}

	go func() {
		sdsServiceLog.Info("Start SDS grpc server for ingress gateway proxy")
		waitTime := time.Second

		for i := 0; i < maxRetryTimes; i++ {
			serverOk := true
			setUpUdsOK := true
			if s.grpcGatewayListener != nil {
				if err = s.grpcGatewayServer.Serve(s.grpcGatewayListener); err != nil {
					sdsServiceLog.Errorf("SDS grpc server for ingress gateway proxy failed to start: %v", err)
					serverOk = false
				}
			}
			if s.grpcGatewayListener == nil {
				if s.grpcGatewayListener, err = setUpUds(options.GatewayUDSPath); err != nil {
					sdsServiceLog.Errorf("SDS grpc server for ingress gateway proxy failed to set up UDS: %v", err)
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

func setUpUds(udsPath string) (net.Listener, error) {
	// Remove unix socket before use.
	if err := os.Remove(udsPath); err != nil && !os.IsNotExist(err) {
		// Anything other than "file not found" is an error.
		sdsServiceLog.Errorf("Failed to remove unix://%s: %v", udsPath, err)
		return nil, fmt.Errorf("failed to remove unix://%s", udsPath)
	}

	// Attempt to create the folder in case it doesn't exist
	if err := os.MkdirAll(filepath.Dir(udsPath), 0750); err != nil {
		// If we cannot create it, just warn here - we will fail later if there is a real error
		sdsServiceLog.Warnf("Failed to create directory for %v: %v", udsPath, err)
	}

	var err error
	udsListener, err := net.Listen("unix", udsPath)
	if err != nil {
		sdsServiceLog.Errorf("Failed to listen on unix socket %q: %v", udsPath, err)
		return nil, err
	}

	// Update SDS UDS file permission so that istio-proxy has permission to access it.
	if _, err := os.Stat(udsPath); err != nil {
		sdsServiceLog.Errorf("SDS uds file %q doesn't exist", udsPath)
		return nil, fmt.Errorf("sds uds file %q doesn't exist", udsPath)
	}
	if err := os.Chmod(udsPath, 0666); err != nil {
		sdsServiceLog.Errorf("Failed to update %q permission", udsPath)
		return nil, fmt.Errorf("failed to update %q permission", udsPath)
	}

	return udsListener, nil
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
