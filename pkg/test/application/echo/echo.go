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

package echo

import (
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/application"
	"istio.io/istio/pkg/test/application/echo/proto"
)

// Factory is a factory for echo applications.
type Factory struct {
	Ports     model.PortList
	TLSCert   string
	TLSCKey   string
	Version   string
	UDSServer string
}

// NewApplication implements the application.Factory interface.
func (f *Factory) NewApplication(dialer application.Dialer) (application.Application, error) {
	// Make a copy of the port list.
	ports := make(model.PortList, len(f.Ports))
	for i, p := range f.Ports {
		tempP := *p
		ports[i] = &tempP
	}

	app := &echo{
		ports:   ports,
		tlsCert: f.TLSCert,
		tlsCKey: f.TLSCKey,
		version: f.Version,
		uds:     f.UDSServer,
		dialer:  dialer.Fill(),
	}
	if err := app.start(); err != nil {
		return nil, err
	}

	return app, nil
}

// echo is a simple application than processes echo requests via various transports.
type echo struct {
	ports   model.PortList
	tlsCert string
	tlsCKey string
	version string
	dialer  application.Dialer
	uds     string

	servers []serverInterface
}

// GetPorts implements the application.Application interface
func (a *echo) GetPorts() model.PortList {
	return a.ports
}

func (a *echo) start() (err error) {
	defer func() {
		if err != nil {
			_ = a.Close()
		}
	}()

	if err = a.validate(); err != nil {
		return err
	}

	a.servers = make([]serverInterface, 0)
	for _, p := range a.ports {
		handler := &handler{
			version: a.version,
			caFile:  a.tlsCert,
			dialer:  a.dialer,
		}
		switch p.Protocol {
		case model.ProtocolTCP, model.ProtocolHTTP, model.ProtocolHTTPS:
			a.servers = append(a.servers, &httpServer{
				port: p,
				h:    handler,
			})
		case model.ProtocolHTTP2, model.ProtocolGRPC:
			a.servers = append(a.servers, &grpcServer{
				port:    p,
				h:       handler,
				tlsCert: a.tlsCert,
				tlsCKey: a.tlsCKey,
			})
		default:
			return fmt.Errorf("unsupported protocol: %s", p.Protocol)
		}
	}

	if len(a.uds) > 0 {
		a.servers = append(a.servers, &httpServer{
			uds: a.uds,
			h: &handler{
				version: a.version,
				caFile:  a.tlsCert,
				dialer:  a.dialer,
			},
		})
	}

	// Start the servers, updating port numbers as necessary.
	for _, s := range a.servers {
		if err := s.start(); err != nil {
			return err
		}
	}
	return nil
}

// Close implements the application.Application interface
func (a *echo) Close() (err error) {
	for i, s := range a.servers {
		if s != nil {
			err = multierror.Append(err, s.stop())
			a.servers[i] = nil
		}
	}
	return
}

func (a *echo) validate() error {
	for _, port := range a.ports {
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

type serverInterface interface {
	start() error
	stop() error
}

type httpServer struct {
	server *http.Server
	port   *model.Port
	uds    string
	h      *handler
}

func (s *httpServer) start() error {
	var listener net.Listener
	var p int
	var err error

	s.server = &http.Server{
		Handler: s.h,
	}

	if len(s.uds) > 0 {
		p = 0
		listener, err = listenOnUDS(s.uds)
	} else {
		// Listen on the given port and update the port if it changed from what was passed in.
		listener, p, err = listenOnPort(s.port.Port)
		// Store the actual listening port back to the argument.
		s.port.Port = p
		s.h.port = p
	}

	if err != nil {
		return err
	}

	if len(s.uds) > 0 {
		fmt.Printf("Listening HTTP/1.1 on %v\n", s.uds)
	} else {
		s.server.Addr = fmt.Sprintf(":%d", p)
		fmt.Printf("Listening HTTP/1.1 on %v\n", p)
	}

	// Start serving HTTP traffic.
	go func() { _ = s.server.Serve(listener) }()
	return nil
}

func (s *httpServer) stop() error {
	return s.server.Close()
}

type grpcServer struct {
	tlsCert string
	tlsCKey string
	port    *model.Port
	h       *handler

	server *grpc.Server
}

func (s *grpcServer) start() error {
	// Listen on the given port and update the port if it changed from what was passed in.
	listener, p, err := listenOnPort(s.port.Port)
	if err != nil {
		return err
	}
	// Store the actual listening port back to the argument.
	s.port.Port = p
	s.h.port = p
	fmt.Printf("Listening GRPC on %v\n", p)

	if s.tlsCert != "" && s.tlsCKey != "" {
		// Create the TLS credentials
		creds, errCreds := credentials.NewServerTLSFromFile(s.tlsCert, s.tlsCKey)
		if errCreds != nil {
			log.Errorf("could not load TLS keys: %s", errCreds)
		}
		s.server = grpc.NewServer(grpc.Creds(creds))
	} else {
		s.server = grpc.NewServer()
	}
	proto.RegisterEchoTestServiceServer(s.server, s.h)

	// Start serving GRPC traffic.
	go func() { _ = s.server.Serve(listener) }()
	return nil
}

func (s *grpcServer) stop() error {
	s.server.Stop()
	return nil
}

func listenOnPort(port int) (net.Listener, int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, 0, err
	}

	port = ln.Addr().(*net.TCPAddr).Port
	return ln, port, nil
}

func listenOnUDS(uds string) (net.Listener, error) {
	_ = os.Remove(uds)
	ln, err := net.Listen("unix", uds)
	if err != nil {
		return nil, err
	}

	return ln, nil
}
