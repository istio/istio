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

package server

import (
	"errors"
	"fmt"

	"istio.io/istio/operator/cmd/shared"
	"istio.io/istio/operator/pkg/kube/operator"
	"istio.io/istio/pkg/ctrlz"
        kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/version"
)

var scope = log.RegisterScope("runtime", "Operator runtime", 0)

// Server is the main entry point into the Operator code.
type Server struct {
	shutdown   chan error
	operator   *operator.Controller
	controlZ   *ctrlz.Server
	stopCh     chan struct{}
}

type patchTable struct {
	logConfigure                func(*log.Options) error
//	newKubeFromConfigFile       func(string) (kube.Interfaces, error)
}

func defaultPatchTable() patchTable {
	return patchTable{
		logConfigure:                log.Configure,
//		newKubeFromConfigFile:       kube.NewKubeFromConfigFile,
	}
}

// New returns a new instance of a Server.
func New(a *Args) (*Server, error) {
	return newServer(a, defaultPatchTable())
}

// newServer creates the various controllers needed for the operator.
func newServer(a *Args, p patchTable) (*Server, error) {
	s := &Server{}
	var err error
	if err = p.logConfigure(a.LoggingOptions); err != nil {
		return nil, err
	}

	k, kuberr := kubelib.BuildClientConfig(a.KubeConfig, "")
	if kuberr != nil {
		return nil, nil //fmt.ErrorF("failed to connect to Kubernetes API.")
	}

	s.stopCh = make(chan struct{})
	s.controlZ, _ = ctrlz.Run(a.IntrospectionOptions, nil)
	s.operator = operator.NewController(k, "istio-operator")

	return s, nil
}

// Run starts the operator controller.
func (s *Server) Run() {
	s.shutdown = make(chan error, 1)
// TODO sdake un-gorotoutine this.
	go func() {
	        s.operator.Run(s.stopCh)
	}()
}

// Wait waits for the server to exit.
func (s *Server) Wait() error {
	if s.shutdown == nil {
		return fmt.Errorf("server not running")
	}

	err := <-s.shutdown
	s.shutdown = nil
	return err
}

// Close cleans up resources used by the server.
func (s *Server) Close() error {
	if s.stopCh != nil {
		close(s.stopCh)
		s.stopCh = nil
	}

	if s.controlZ != nil {
		s.controlZ.Close()
	}

	// final attempt to purge buffered logs
	_ = log.Sync()

	return nil
}

//RunServer starts operator in server mode
func RunServer(sa *Args, printf, fatalf shared.FormatFn, livenessProbeController,
	readinessProbeController probe.Controller) {
	printf("Operator started with\n%s", sa)
	s, err := New(sa)
	if err != nil {
		fatalf("Unable to initialize Operator Server: %v", err)
	}
	printf("Istio Operator: %s", version.Info)
	s.Run()
	if livenessProbeController != nil {
		serverLivenessProbe := probe.NewProbe()
		serverLivenessProbe.SetAvailable(nil)
		serverLivenessProbe.RegisterProbe(livenessProbeController, "serverLiveness")
		defer serverLivenessProbe.SetAvailable(errors.New("stopped"))
	}
	if readinessProbeController != nil {
		serverReadinessProbe := probe.NewProbe()
		serverReadinessProbe.SetAvailable(nil)
		serverReadinessProbe.RegisterProbe(readinessProbeController, "serverReadiness")
		defer serverReadinessProbe.SetAvailable(errors.New("stopped"))
	}

	err = s.Wait()
	if err != nil {
		fatalf("Operator Server unexpectedly terminated: %v", err)
	}
	_ = s.Close()
}
