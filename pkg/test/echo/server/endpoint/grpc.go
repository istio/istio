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

package endpoint

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"

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

func (s *grpcInstance) GetConfig() Config {
	return s.Config
}

func (s *grpcInstance) Start(onReady OnReadyFunc) error {
	// Listen on the given port and update the port if it changed from what was passed in.
	listener, p, err := listenOnAddress(s.ListenerIP, s.Port.Port)
	if err != nil {
		return err
	}
	// Store the actual listening port back to the argument.
	s.Port.Port = p

	if s.Port.TLS {
		fmt.Printf("Listening GRPC (over TLS) on %v\n", p)
		// Create the TLS credentials
		creds, errCreds := credentials.NewServerTLSFromFile(s.TLSCert, s.TLSKey)
		if errCreds != nil {
			epLog.Errorf("could not load TLS keys: %s", errCreds)
		}
		s.server = grpc.NewServer(grpc.Creds(creds))
	} else {
		fmt.Printf("Listening GRPC on %v\n", p)
		s.server = grpc.NewServer()
	}
	proto.RegisterEchoTestServiceServer(s.server, &grpcHandler{
		Config: s.Config,
	})
	reflection.Register(s.server)

	// Start serving GRPC traffic.
	go func() {
		err := s.server.Serve(listener)
		epLog.Warnf("Port %d listener terminated with error: %v", p, err)
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
		epLog.Errorf("readiness failed for GRPC endpoint %s: %v", listener.Addr().String(), err)
	} else {
		epLog.Infof("ready for GRPC endpoint %s", listener.Addr().String())
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
	defer common.Metrics.GrpcRequests.With(common.PortLabel.Value(strconv.Itoa(h.Port.Port))).Increment()
	body := bytes.Buffer{}
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		for key, values := range md {
			if strings.HasSuffix(key, "-bin") {
				continue
			}
			field := response.Field(key)
			if key == ":authority" {
				field = response.HostField
			}
			for _, value := range values {
				writeField(&body, field, value)
			}
		}
	}

	id := uuid.New()
	epLog.WithLabels("message", req.GetMessage(), "headers", md, "id", id).Infof("GRPC Request")

	portNumber := 0
	if h.Port != nil {
		portNumber = h.Port.Port
	}

	ip := "0.0.0.0"
	if peerInfo, ok := peer.FromContext(ctx); ok {
		ip, _, _ = net.SplitHostPort(peerInfo.Addr.String())
	}

	writeField(&body, response.StatusCodeField, response.StatusCodeOK)
	writeField(&body, response.ServiceVersionField, h.Version)
	writeField(&body, response.ServicePortField, strconv.Itoa(portNumber))
	writeField(&body, response.ClusterField, h.Cluster)
	writeField(&body, response.IPField, ip)
	writeField(&body, response.IstioVersionField, h.IstioVersion)
	writeField(&body, "Echo", req.GetMessage())

	if hostname, err := os.Hostname(); err == nil {
		writeField(&body, response.HostnameField, hostname)
	}

	epLog.WithLabels("id", id).Infof("GRPC Response")
	return &proto.EchoResponse{Message: body.String()}, nil
}

func (h *grpcHandler) ForwardEcho(ctx context.Context, req *proto.ForwardEchoRequest) (*proto.ForwardEchoResponse, error) {
	id := uuid.New()
	l := epLog.WithLabels("url", req.Url, "id", id)
	l.Infof("ForwardEcho request")
	t0 := time.Now()
	instance, err := forwarder.New(forwarder.Config{
		Request: req,
		Dialer:  h.Dialer,
	})
	if err != nil {
		return nil, err
	}
	defer instance.Close()

	ret, err := instance.Run(ctx)
	if err == nil {
		l.WithLabels("latency", time.Since(t0)).Infof("ForwardEcho response complete: %v", ret.GetOutput())
	} else {
		l.WithLabels("latency", time.Since(t0)).Infof("ForwardEcho response failed: %v", err)
	}
	return ret, err
}
