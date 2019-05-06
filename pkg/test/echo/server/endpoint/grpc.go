// Copyright 2019 Istio Authors
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

package endpoint

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/echo/server/forwarder"
	"istio.io/istio/pkg/test/util/retry"
)

var _ Instance = &grpcInstance{}

type grpcInstance struct {
	Config
	server *grpc.Server
}

func newGRPC(config Config) Instance {
	return &grpcInstance{
		Config: config,
	}
}

func (s *grpcInstance) Start(onReady OnReadyFunc) error {
	// Listen on the given port and update the port if it changed from what was passed in.
	listener, p, err := listenOnPort(s.Port.Port)
	if err != nil {
		return err
	}
	// Store the actual listening port back to the argument.
	s.Port.Port = p
	fmt.Printf("Listening GRPC on %v\n", p)

	if s.TLSCert != "" && s.TLSKey != "" {
		// Create the TLS credentials
		creds, errCreds := credentials.NewServerTLSFromFile(s.TLSCert, s.TLSKey)
		if errCreds != nil {
			log.Errorf("could not load TLS keys: %s", errCreds)
		}
		s.server = grpc.NewServer(grpc.Creds(creds))
	} else {
		s.server = grpc.NewServer()
	}
	proto.RegisterEchoTestServiceServer(s.server, &grpcHandler{
		Config: s.Config,
	})

	// Start serving GRPC traffic.
	go func() {
		_ = s.server.Serve(listener)
	}()

	// Notify the WaitGroup once the port has transitioned to ready.
	go s.awaitReady(onReady, listener)

	return nil
}

func (s *grpcInstance) awaitReady(onReady OnReadyFunc, listener net.Listener) {
	defer onReady()

	err := retry.UntilSuccess(func() error {
		f, err := forwarder.New(forwarder.Config{
			Request: &proto.ForwardEchoRequest{
				Url:           "grpc://" + listener.Addr().String(),
				Message:       "hello",
				TimeoutMicros: common.DurationToMicros(readyInterval),
			},
		})
		defer func() {
			_ = f.Close()
		}()

		if err != nil {
			return err
		}
		_, err = f.Run(context.Background())
		return err
	}, retry.Timeout(readyTimeout), retry.Delay(readyInterval))
	if err != nil {
		log.Errorf("readiness failed for GRPC endpoint %s: %v", listener.Addr().String(), err)
	} else {
		log.Infof("ready for GRPC endpoint %s", listener.Addr().String())
	}
}

func (s *grpcInstance) Close() error {
	if s.server != nil {
		s.server.Stop()
	}
	return nil
}

type grpcHandler struct {
	Config
}

func (h *grpcHandler) Echo(ctx context.Context, req *proto.EchoRequest) (*proto.EchoResponse, error) {
	body := bytes.Buffer{}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		for key, values := range md {
			field := response.Field(key)
			if key == ":authority" {
				field = response.HostField
			}
			for _, value := range values {
				writeField(&body, field, value)
			}
		}
	}
	portNumber := 0
	if h.Port != nil {
		portNumber = h.Port.Port
	}

	writeField(&body, response.StatusCodeField, response.StatusCodeOK)
	writeField(&body, response.ServiceVersionField, h.Version)
	writeField(&body, response.ServicePortField, strconv.Itoa(portNumber))
	writeField(&body, response.Field("Echo"), req.GetMessage())

	if hostname, err := os.Hostname(); err == nil {
		writeField(&body, response.HostnameField, hostname)
	}

	return &proto.EchoResponse{Message: body.String()}, nil
}

func (h *grpcHandler) ForwardEcho(ctx context.Context, req *proto.ForwardEchoRequest) (*proto.ForwardEchoResponse, error) {
	instance, err := forwarder.New(forwarder.Config{
		Request: req,
		Dialer:  h.Dialer,
		TLSCert: h.TLSCert,
	})
	if err != nil {
		return nil, err
	}

	return instance.Run(ctx)
}
