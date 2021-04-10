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

package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	corev2 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	authv2 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v2"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	typev2 "github.com/envoyproxy/go-control-plane/envoy/type"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/gogo/googleapis/google/rpc"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
)

const (
	checkHeader   = "x-ext-authz"
	allowedValue  = "allow"
	resultHeader  = "x-ext-authz-check-result"
	resultAllowed = "allowed"
	resultDenied  = "denied"
)

var (
	serviceAccount = flag.String("allow_service_account", "a", "allowed service account, matched against the service account in the source principal from the client certificate")
	httpPort       = flag.String("http", "8000", "HTTP server port")
	grpcPort       = flag.String("grpc", "9000", "gRPC server port")
	denyBody       = fmt.Sprintf("denied by ext_authz for not found header `%s: %s` in the request", checkHeader, allowedValue)
)

type extAuthzServerV2 struct{}
type extAuthzServerV3 struct{}

// ExtAuthzServer implements the ext_authz v2/v3 gRPC and HTTP check request API.
type ExtAuthzServer struct {
	grpcServer *grpc.Server
	httpServer *http.Server
	grpcV2     *extAuthzServerV2
	grpcV3     *extAuthzServerV3
	// For test only
	httpPort chan int
	grpcPort chan int
}

// Check implements gRPC v2 check request.
func (s *extAuthzServerV2) Check(ctx context.Context, request *authv2.CheckRequest) (*authv2.CheckResponse, error) {
	l := fmt.Sprintf("%s%s, attributes: %v\n",
		request.GetAttributes().GetRequest().GetHttp().GetHost(),
		request.GetAttributes().GetRequest().GetHttp().GetPath(),
		request.GetAttributes())
	if allowedValue == request.GetAttributes().GetRequest().GetHttp().GetHeaders()[checkHeader] || (request.GetAttributes().Source != nil && strings.HasSuffix(request.GetAttributes().Source.Principal, "/sa/"+*serviceAccount)) {
		log.Printf("[gRPCv2][allowed]: %s", l)
		return &authv2.CheckResponse{
			HttpResponse: &authv2.CheckResponse_OkResponse{
				OkResponse: &authv2.OkHttpResponse{
					Headers: []*corev2.HeaderValueOption{
						{
							Header: &corev2.HeaderValue{
								Key:   resultHeader,
								Value: resultAllowed,
							},
						},
					},
				},
			},
			Status: &status.Status{Code: int32(rpc.OK)},
		}, nil
	}

	log.Printf("[gRPCv2][denied]: %s", l)
	return &authv2.CheckResponse{
		HttpResponse: &authv2.CheckResponse_DeniedResponse{
			DeniedResponse: &authv2.DeniedHttpResponse{
				Status: &typev2.HttpStatus{Code: typev2.StatusCode_Forbidden},
				Body:   denyBody,
				Headers: []*corev2.HeaderValueOption{
					{
						Header: &corev2.HeaderValue{
							Key:   resultHeader,
							Value: resultDenied,
						},
					},
				},
			},
		},
		Status: &status.Status{Code: int32(rpc.PERMISSION_DENIED)},
	}, nil
}

// Check implements gRPC v3 check request.
func (s *extAuthzServerV3) Check(ctx context.Context, request *authv3.CheckRequest) (*authv3.CheckResponse, error) {
	l := fmt.Sprintf("%s%s, attributes: %v\n",
		request.GetAttributes().GetRequest().GetHttp().GetHost(),
		request.GetAttributes().GetRequest().GetHttp().GetPath(),
		request.GetAttributes())
	if allowedValue == request.GetAttributes().GetRequest().GetHttp().GetHeaders()[checkHeader] || (request.GetAttributes().Source != nil && strings.HasSuffix(request.GetAttributes().Source.Principal, "/sa/"+*serviceAccount)) {
		log.Printf("[gRPCv3][allowed]: %s", l)
		return &authv3.CheckResponse{
			HttpResponse: &authv3.CheckResponse_OkResponse{
				OkResponse: &authv3.OkHttpResponse{
					Headers: []*corev3.HeaderValueOption{
						{
							Header: &corev3.HeaderValue{
								Key:   resultHeader,
								Value: resultAllowed,
							},
						},
					},
				},
			},
			Status: &status.Status{Code: int32(rpc.OK)},
		}, nil
	}

	log.Printf("[gRPCv3][denied]: %s", l)
	return &authv3.CheckResponse{
		HttpResponse: &authv3.CheckResponse_DeniedResponse{
			DeniedResponse: &authv3.DeniedHttpResponse{
				Status: &typev3.HttpStatus{Code: typev3.StatusCode_Forbidden},
				Body:   denyBody,
				Headers: []*corev3.HeaderValueOption{
					{
						Header: &corev3.HeaderValue{
							Key:   resultHeader,
							Value: resultDenied,
						},
					},
				},
			},
		},
		Status: &status.Status{Code: int32(rpc.PERMISSION_DENIED)},
	}, nil
}

// ServeHTTP implements the HTTP check request.
func (s *ExtAuthzServer) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	l := fmt.Sprintf("%s %s%s, headers: %v\n", request.Method, request.Host, request.URL, request.Header)
	if allowedValue == request.Header.Get(checkHeader) {
		log.Printf("[HTTP][allowed]: %s", l)
		response.Header().Set(resultHeader, resultAllowed)
		response.WriteHeader(http.StatusOK)
	} else {
		log.Printf("[HTTP][denied]: %s", l)
		response.Header().Set(resultHeader, resultDenied)
		response.WriteHeader(http.StatusForbidden)
		response.Write([]byte(denyBody))
	}
}

func (s *ExtAuthzServer) startGRPC(address string, wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
		log.Printf("Stopped gRPC server")
	}()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Failed to start gRPC server: %v", err)
		return
	}
	// Store the port for test only.
	s.grpcPort <- listener.Addr().(*net.TCPAddr).Port

	s.grpcServer = grpc.NewServer()
	authv2.RegisterAuthorizationServer(s.grpcServer, s.grpcV2)
	authv3.RegisterAuthorizationServer(s.grpcServer, s.grpcV3)

	log.Printf("Starting gRPC server at %s", listener.Addr())
	if err := s.grpcServer.Serve(listener); err != nil {
		log.Fatalf("Failed to serve gRPC server: %v", err)
		return
	}
}

func (s *ExtAuthzServer) startHTTP(address string, wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
		log.Printf("Stopped HTTP server")
	}()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Failed to create HTTP server: %v", err)
	}
	// Store the port for test only.
	s.httpPort <- listener.Addr().(*net.TCPAddr).Port
	s.httpServer = &http.Server{Handler: s}

	log.Printf("Starting HTTP server at %s", listener.Addr())
	if err := s.httpServer.Serve(listener); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}

func (s *ExtAuthzServer) run(httpAddr, grpcAddr string) {
	var wg sync.WaitGroup
	wg.Add(2)
	go s.startHTTP(httpAddr, &wg)
	go s.startGRPC(grpcAddr, &wg)
	wg.Wait()
}

func (s *ExtAuthzServer) stop() {
	s.grpcServer.Stop()
	log.Printf("GRPC server stopped")
	log.Printf("HTTP server stopped: %v", s.httpServer.Close())
}

func NewExtAuthzServer() *ExtAuthzServer {
	return &ExtAuthzServer{
		grpcV2:   &extAuthzServerV2{},
		grpcV3:   &extAuthzServerV3{},
		httpPort: make(chan int, 1),
		grpcPort: make(chan int, 1),
	}
}

func main() {
	flag.Parse()
	s := NewExtAuthzServer()
	go s.run(fmt.Sprintf(":%s", *httpPort), fmt.Sprintf(":%s", *grpcPort))
	defer s.stop()

	// Wait for the process to be shutdown.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}
