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

package forwarder

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/security/pkg/pki/util"
)

type testEchoServer struct {
	proto.UnimplementedEchoTestServiceServer
}

func (testEchoServer) Echo(_ context.Context, req *proto.EchoRequest) (*proto.EchoResponse, error) {
	return &proto.EchoResponse{Message: req.Message}, nil
}

func TestGRPCS(t *testing.T) {
	certPEM, keyPEM, err := util.GenCertKeyFromOptions(util.CertOptions{
		Host:         "127.0.0.1",
		TTL:          time.Hour,
		IsSelfSigned: true,
		IsServer:     true,
		Org:          "istio.io",
		ECSigAlg:     util.EcdsaSigAlg,
	})
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})))
	proto.RegisterEchoTestServiceServer(server, testEchoServer{})
	go func() {
		_ = server.Serve(l)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = l.Close()
	})

	f := New()
	t.Cleanup(func() { _ = f.Close() })
	resp, err := f.ForwardEcho(context.Background(), &Config{Request: &proto.ForwardEchoRequest{
		Url:     "grpcs://" + l.Addr().String(),
		Message: "hello",
		CaCert:  string(certPEM),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("got %d responses, want 1", len(resp.Output))
	}
}
