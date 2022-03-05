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
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/admin"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/health"
	grpcHealth "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/xds"
	"k8s.io/utils/env"

	"istio.io/istio/pkg/istio-agent/grpcxds"
	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/echo/server/forwarder"
	"istio.io/istio/pkg/test/util/retry"
)

var _ Instance = &grpcInstance{}

// grpcServer is the intersection of used methods for grpc.Server and xds.GRPCServer
type grpcServer interface {
	reflection.GRPCServer
	Serve(listener net.Listener) error
	Stop()
}

type grpcInstance struct {
	Config
	server   grpcServer
	cleanups []func()
}

func newGRPC(config Config) Instance {
	return &grpcInstance{
		Config: config,
	}
}

func (s *grpcInstance) GetConfig() Config {
	return s.Config
}

func (s *grpcInstance) newServer(opts ...grpc.ServerOption) grpcServer {
	if s.Port.XDSServer {
		if len(s.Port.XDSTestBootstrap) > 0 {
			opts = append(opts, xds.BootstrapContentsForTesting(s.Port.XDSTestBootstrap))
		}
		epLog.Infof("Using xDS for serverside gRPC on %d", s.Port.Port)
		return xds.NewGRPCServer(opts...)
	}
	return grpc.NewServer(opts...)
}

func (s *grpcInstance) Start(onReady OnReadyFunc) error {
	// Listen on the given port and update the port if it changed from what was passed in.
	listener, p, err := listenOnAddress(s.ListenerIP, s.Port.Port)
	if err != nil {
		return err
	}
	// Store the actual listening port back to the argument.
	s.Port.Port = p

	var opts []grpc.ServerOption
	if s.Port.TLS {
		epLog.Infof("Listening GRPC (over TLS) on %v", p)
		// Create the TLS credentials
		creds, errCreds := credentials.NewServerTLSFromFile(s.TLSCert, s.TLSKey)
		if errCreds != nil {
			epLog.Errorf("could not load TLS keys: %s", errCreds)
		}
		opts = append(opts, grpc.Creds(creds))
	} else if s.Port.XDSServer {
		epLog.Infof("Listening GRPC (over xDS-configured mTLS) on %v", p)
		creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{
			FallbackCreds: insecure.NewCredentials(),
		})
		if err != nil {
			return err
		}
		opts = append(opts, grpc.Creds(creds))
	} else {
		epLog.Infof("Listening GRPC on %v", p)
	}
	s.server = s.newServer(opts...)

	// add the standard grpc health check
	healthServer := health.NewServer()
	grpcHealth.RegisterHealthServer(s.server, healthServer)

	proto.RegisterEchoTestServiceServer(s.server, &EchoGrpcHandler{
		Config: s.Config,
	})
	reflection.Register(s.server)
	if val, _ := env.GetBool("EXPOSE_GRPC_ADMIN", false); val {
		cleanup, err := admin.Register(s.server)
		if err != nil {
			return err
		}
		s.cleanups = append(s.cleanups, cleanup)
	}
	// Start serving GRPC traffic.
	go func() {
		err := s.server.Serve(listener)
		epLog.Warnf("Port %d listener terminated with error: %v", p, err)
	}()

	// Notify the WaitGroup once the port has transitioned to ready.
	go s.awaitReady(func() {
		healthServer.SetServingStatus("", grpcHealth.HealthCheckResponse_SERVING)
		onReady()
	}, listener)

	return nil
}

func (s *grpcInstance) awaitReady(onReady OnReadyFunc, listener net.Listener) {
	defer onReady()

	err := retry.UntilSuccess(func() error {
		cert, key, ca, err := s.certsFromBootstrapForReady()
		if err != nil {
			return err
		}
		req := &proto.ForwardEchoRequest{
			Url:           "grpc://" + listener.Addr().String(),
			Message:       "hello",
			TimeoutMicros: common.DurationToMicros(readyInterval),
		}
		if s.Port.XDSReadinessTLS {
			// TODO: using the servers key/cert is not always valid, it may not be allowed to make requests to itself
			req.CertFile = cert
			req.KeyFile = key
			req.CaCertFile = ca
			req.InsecureSkipVerify = true
		}
		f, err := forwarder.New(forwarder.Config{
			XDSTestBootstrap: s.Port.XDSTestBootstrap,
			Request:          req,
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

// TODO (hack) we have to send certs OR use xds:///fqdn. We don't know our own fqdn, and even if we did
// we could send traffic to another instance. Instead we look into gRPC internals to authenticate with ourself.
func (s *grpcInstance) certsFromBootstrapForReady() (cert string, key string, ca string, err error) {
	if !s.Port.XDSServer {
		return
	}

	var bootstrapData []byte
	if data := s.Port.XDSTestBootstrap; len(data) > 0 {
		bootstrapData = data
	} else if path := os.Getenv("GRPC_XDS_BOOTSTRAP"); len(path) > 0 {
		bootstrapData, err = os.ReadFile(path)
	} else if data := os.Getenv("GRPC_XDS_BOOTSTRAP_CONFIG"); len(data) > 0 {
		bootstrapData = []byte(data)
	}
	var bootstrap grpcxds.Bootstrap
	if uerr := json.Unmarshal(bootstrapData, &bootstrap); uerr != nil {
		err = uerr
		return
	}
	certs := bootstrap.FileWatcherProvider()
	if certs == nil {
		err = fmt.Errorf("no certs found in bootstrap")
		return
	}
	cert = certs.CertificateFile
	key = certs.PrivateKeyFile
	ca = certs.CACertificateFile
	return
}

func (s *grpcInstance) Close() error {
	if s.server != nil {
		s.server.Stop()
	}
	for _, cleanup := range s.cleanups {
		cleanup()
	}
	return nil
}

type EchoGrpcHandler struct {
	proto.UnimplementedEchoTestServiceServer
	Config
}

func (h *EchoGrpcHandler) Echo(ctx context.Context, req *proto.EchoRequest) (*proto.EchoResponse, error) {
	defer common.Metrics.GrpcRequests.With(common.PortLabel.Value(strconv.Itoa(h.Port.Port))).Increment()
	body := bytes.Buffer{}
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		for key, values := range md {
			if strings.HasSuffix(key, "-bin") {
				// Skip binary headers.
				continue
			}

			field := key

			if key == ":authority" {
				for _, value := range values {
					writeField(&body, echo.HostField, value)
				}
			}

			for _, value := range values {
				writeRequestHeader(&body, field, value)
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

	writeField(&body, echo.StatusCodeField, strconv.Itoa(http.StatusOK))
	writeField(&body, echo.ServiceVersionField, h.Version)
	writeField(&body, echo.ServicePortField, strconv.Itoa(portNumber))
	writeField(&body, echo.ClusterField, h.Cluster)
	writeField(&body, echo.IPField, ip)
	writeField(&body, echo.IstioVersionField, h.IstioVersion)
	writeField(&body, echo.ProtocolField, "GRPC")
	writeField(&body, "Echo", req.GetMessage())

	if hostname, err := os.Hostname(); err == nil {
		writeField(&body, echo.HostnameField, hostname)
	}

	epLog.WithLabels("id", id).Infof("GRPC Response")
	return &proto.EchoResponse{Message: body.String()}, nil
}

func (h *EchoGrpcHandler) ForwardEcho(ctx context.Context, req *proto.ForwardEchoRequest) (*proto.ForwardEchoResponse, error) {
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
