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

package mock

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "istio.io/api/security/v1alpha1"
)

// FakeExternalCA fake gRPC server for Istio Signing API
type FakeExternalCA struct {
	pb.UnimplementedIstioCertificateServiceServer
	g            *grpc.Server
	workloadCert string
	rootCert     string
}

// NewFakeExternalCA create fake gRPC server for Istio Signing API
func NewFakeExternalCA(enableTLS bool, serverCert string, serverKey string, rootCert string,
	workloadCertFile string) (*FakeExternalCA, error) {

	if !enableTLS {
		f := &FakeExternalCA{
			g: grpc.NewServer(),
		}
		pb.RegisterIstioCertificateServiceServer(f.g, f)
		return f, nil
	}

	certificate, err := tls.LoadX509KeyPair(serverCert, serverKey)
	if err != nil {
		return nil, err
	}

	rootCAs := x509.NewCertPool()
	rootBytes, err := ioutil.ReadFile(rootCert)
	if err != nil {
		return nil, err
	}

	if ok := rootCAs.AppendCertsFromPEM(rootBytes); !ok {
		return nil, fmt.Errorf("cannot read root cert from: %v", rootCert)
	}

	workloadCert, err := ioutil.ReadFile(workloadCertFile)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    rootCAs,
	}

	f := &FakeExternalCA{
		g:            grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig))),
		rootCert:     string(rootBytes),
		workloadCert: string(workloadCert),
	}
	pb.RegisterIstioCertificateServiceServer(f.g, f)
	return f, nil
}

// Serve start listen on random port
func (f *FakeExternalCA) Serve() (net.Addr, error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	go func() {
		err := f.g.Serve(lis)
		if err != nil {
			fmt.Println(fmt.Errorf("error on start Fake CA Server: %v", err))
		}
	}()

	return lis.Addr(), nil
}

// Stop stop grpc server
func (f *FakeExternalCA) Stop() {
	f.g.Stop()
}

// CreateCertificate fake response cert chain
func (f *FakeExternalCA) CreateCertificate(ctx context.Context,
	req *pb.IstioCertificateRequest) (*pb.IstioCertificateResponse, error) {
	time.Sleep(1 * time.Second)

	return &pb.IstioCertificateResponse{
		CertChain: []string{f.workloadCert, f.rootCert},
	}, nil
}
