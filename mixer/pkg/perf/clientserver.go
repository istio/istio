// Copyright 2017 Istio Authors
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

package perf

import (
	"net"
	"net/http"
	"net/rpc"

	"istio.io/istio/mixer/pkg/log"
)

// ClientServer is an RPC server that the Controller connects to remotely control a Mixer perf test client.
type ClientServer struct {
	client *client

	rpcServer *rpc.Server
	listener  net.Listener
	rpcPath   string

	shutdown chan struct{}
}

// Wait blocks until the server is instructed to exit. This should be called only once.
func (s *ClientServer) Wait() {
	<-s.shutdown
}

// NewClientServer creates a new ClientServer and returns it. Before doing so, it connects to the controller and registers
// itself with it.
func NewClientServer(controllerLoc ServiceLocation) (*ClientServer, error) {
	c := &client{}

	server := &ClientServer{
		client:   c,
		shutdown: make(chan struct{}, 1),
	}

	if err := server.initializeRPCServer(); err != nil {
		server.close()
		return nil, err
	}

	if err := server.registerWithController(controllerLoc); err != nil {
		server.close()
		return nil, err
	}

	return server, nil
}

func (s *ClientServer) registerWithController(controllerLoc ServiceLocation) error {
	log.Infof("ClientServer dialing to controller at: %s", controllerLoc)
	defer log.Sync()
	controller, err := rpc.DialHTTPPath("tcp", controllerLoc.Address, controllerLoc.Path)
	if err != nil {
		return err
	}
	log.Info("ClientServer connected to controller")

	err = controller.Call("Controller.RegisterClient", s.location(), nil)

	// Close the controller connection in any case. We don't need it after the initial registration.
	_ = controller.Close()

	if err != nil {
		return err
	}

	log.Info("ClientServer registered with controller")
	return nil
}

func (s *ClientServer) initializeRPCServer() error {
	// Setup ClientServer's rpc rpcServer first. We will publish this to the controller next.
	var err error
	if s.listener, err = net.Listen("tcp", "127.0.0.1:"); err != nil {
		return err
	}

	s.rpcPath = generatePath("client", s.listener.Addr())
	rpcDebugPath := generateDebugPath("client", s.listener.Addr())

	s.rpcServer = rpc.NewServer()
	s.rpcServer.Register(s)
	s.rpcServer.HandleHTTP(s.rpcPath, rpcDebugPath)

	go http.Serve(s.listener, nil)

	log.Infof("ClientServer listening on: %s", s.location())
	log.Sync()
	return nil
}

func (s *ClientServer) location() ServiceLocation {
	return ServiceLocation{Address: s.listener.Addr().String(), Path: s.rpcPath}
}

func (s *ClientServer) close() (err error) {
	if s.client != nil {
		err = s.client.close()
		s.client = nil
	}
	if s.shutdown != nil {
		s.shutdown <- struct{}{}
		close(s.shutdown)
	}
	if s.listener != nil {
		_ = s.listener.Close()
		s.listener = nil
	}
	return err
}

// ClientServerInitParams is a collection of parameters that are passed as part of the InitializeClient call.
type ClientServerInitParams struct {

	// Setup is the YAML-serialized setup object.
	Setup []byte

	// Address of the Mixer Server.
	Address string
}

// InitializeClient is a remote RPC call that is invoked by the controller to initiate setup of the client environment.
// The Mixer client connects to the server at the given address, and keeps the setup metadata to generate load during
// upcoming run requests.
func (s *ClientServer) InitializeClient(params ClientServerInitParams, _ *struct{}) error {
	log.Infof("ClientServer initializing with server address: %s", params.Address)
	log.Sync()

	var setup Setup
	if err := unmarshallSetup(params.Setup, &setup); err != nil {
		return err
	}
	return s.client.initialize(params.Address, &setup)
}

// Shutdown is a remote RPC call that is invoked by the controller after the benchmark execution has completed.
func (s *ClientServer) Shutdown(struct{}, *struct{}) error {
	log.Info("ClientServer shutting down")
	log.Sync()
	s.client.close()
	s.close()
	return nil
}

// Run is a remote RPC call that is invoked by the controller to request the mixer to run the load for the specified
// number of iterations.
func (s *ClientServer) Run(iterations int, _ *struct{}) error {
	log.Infof("ClientServer running with iterations: %d", iterations)
	// Deliberately not syncing the log here to avoid polluting the benchmark timings.
	return s.client.run(iterations)
}
