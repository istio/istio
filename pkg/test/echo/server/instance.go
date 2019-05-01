//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/server/endpoint"
)

// Config for an echo server Instance.
type Config struct {
	Ports     model.PortList
	TLSCert   string
	TLSKey    string
	Version   string
	UDSServer string
	Dialer    common.Dialer
}

var _ io.Closer = &Instance{}

// Instance of the Echo server.
type Instance struct {
	Config

	endpoints []endpoint.Instance
	ready     uint32
}

// New creates a new server instance.
func New(config Config) *Instance {
	config.Dialer = config.Dialer.FillInDefaults()

	return &Instance{
		Config: config,
	}
}

// Start the server.
func (s *Instance) Start() (err error) {
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()

	if err = s.validate(); err != nil {
		return err
	}

	s.endpoints = make([]endpoint.Instance, 0)
	for _, p := range s.Ports {
		ep, err := s.newEndpoint(p, "")
		if err != nil {
			return err
		}
		s.endpoints = append(s.endpoints, ep)
	}

	if len(s.UDSServer) > 0 {
		ep, err := s.newEndpoint(nil, s.UDSServer)
		if err != nil {
			return err
		}
		s.endpoints = append(s.endpoints, ep)
	}

	return s.waitUntilReady()
}

// Close implements the application.Application interface
func (s *Instance) Close() (err error) {
	for _, s := range s.endpoints {
		if s != nil {
			err = multierror.Append(err, s.Close())
		}
	}
	return
}

func (s *Instance) newEndpoint(port *model.Port, udsServer string) (endpoint.Instance, error) {
	return endpoint.New(endpoint.Config{
		Port:          port,
		UDSServer:     udsServer,
		IsServerReady: s.isReady,
		Version:       s.Version,
		TLSCert:       s.TLSCert,
		TLSKey:        s.TLSKey,
		Dialer:        s.Dialer,
	})
}

func (s *Instance) isReady() bool {
	return atomic.LoadUint32(&s.ready) == 1
}

func (s *Instance) waitUntilReady() error {
	wg := &sync.WaitGroup{}

	onEndpointReady := func() {
		wg.Done()
	}

	// Start the servers, updating port numbers as necessary.
	for _, ep := range s.endpoints {
		wg.Add(1)
		if err := ep.Start(onEndpointReady); err != nil {
			return err
		}
	}

	// Wait for all the servers to start.
	wg.Wait()

	// Indicate that the server is now ready.
	atomic.StoreUint32(&s.ready, 1)

	log.Info("Echo server is now ready")
	return nil
}

func (s *Instance) validate() error {
	for _, port := range s.Ports {
		switch port.Protocol {
		case model.ProtocolTCP:
		case model.ProtocolHTTP:
		case model.ProtocolHTTPS:
		case model.ProtocolHTTP2:
		case model.ProtocolGRPC:
		default:
			return fmt.Errorf("protocol %v not currently supported", port.Protocol)
		}
	}
	return nil
}
