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
	"net"
	"time"

	"go.uber.org/atomic"
	"google.golang.org/grpc"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/uds"
)

const (
	maxStreams    = 100000
	maxRetryTimes = 5
)

// Server is the gPRC server that exposes SDS through UDS.
type Server struct {
	workloadSds *sdsservice

	grpcWorkloadListener net.Listener

	grpcWorkloadServer *grpc.Server
	stopped            *atomic.Bool
}

// NewServer creates and starts the Grpc server for SDS.
func NewServer(options *security.Options, workloadSecretCache security.SecretManager, pkpConf *mesh.PrivateKeyProvider) *Server {
	s := &Server{stopped: atomic.NewBool(false)}
	s.workloadSds = newSDSService(workloadSecretCache, options, pkpConf)
	s.initWorkloadSdsService(options)
	return s
}

func (s *Server) OnSecretUpdate(resourceName string) {
	if s.workloadSds == nil {
		return
	}

	sdsServiceLog.Debugf("Trigger on secret update, resource name: %s", resourceName)
	s.workloadSds.push(resourceName)
}

// Stop closes the gRPC server and debug server.
func (s *Server) Stop() {
	if s == nil {
		return
	}
	s.stopped.Store(true)
	if s.grpcWorkloadServer != nil {
		s.grpcWorkloadServer.Stop()
	}
	if s.grpcWorkloadListener != nil {
		s.grpcWorkloadListener.Close()
	}
	if s.workloadSds != nil {
		s.workloadSds.Close()
	}
}

// initWorkloadSdsService initializes the SDS service, bound to a UDS. The path may be configured.
func (s *Server) initWorkloadSdsService(opts *security.Options) {
	s.grpcWorkloadServer = grpc.NewServer(s.grpcServerOptions()...)
	s.workloadSds.register(s.grpcWorkloadServer)
	var err error
	path := security.GetIstioSDSServerSocketPath()
	if opts.ServeOnlyFiles {
		path = security.FileCredentialNameSocketPath
	}
	s.grpcWorkloadListener, err = uds.NewListener(path)
	go func() {
		sdsServiceLog.Info("Starting SDS grpc server")
		waitTime := time.Second
		started := false
		for i := 0; i < maxRetryTimes; i++ {
			if s.stopped.Load() {
				return
			}
			serverOk := true
			setUpUdsOK := true
			if s.grpcWorkloadListener == nil {
				if s.grpcWorkloadListener, err = uds.NewListener(path); err != nil {
					sdsServiceLog.Errorf("SDS grpc server for workload proxies failed to set up UDS: %v", err)
					setUpUdsOK = false
				}
			}
			if s.grpcWorkloadListener != nil {
				if opts.ServeOnlyFiles {
					sdsServiceLog.Infof("Starting SDS server for file certificates only, will listen on %q", path)
				} else {
					sdsServiceLog.Infof("Starting SDS server for workload certificates, will listen on %q", path)
				}
				if err = s.grpcWorkloadServer.Serve(s.grpcWorkloadListener); err != nil {
					sdsServiceLog.Errorf("SDS grpc server for workload proxies failed to start: %v", err)
					serverOk = false
				}
			}
			if serverOk && setUpUdsOK {
				started = true
				break
			}
			time.Sleep(waitTime)
			waitTime *= 2
		}
		if !started {
			sdsServiceLog.Warn("SDS grpc server could not be started")
		}
	}()
}

func (s *Server) grpcServerOptions() []grpc.ServerOption {
	grpcOptions := []grpc.ServerOption{
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
	}

	return grpcOptions
}
