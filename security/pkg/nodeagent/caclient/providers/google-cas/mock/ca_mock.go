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
	"errors"
	"net"
	"path"
	"strings"
	"time"

	privatecapb "google.golang.org/genproto/googleapis/cloud/security/privateca/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const (
	bufSize = 1024 * 1024
)

var lis *bufconn.Listener

type ContextDialer func(ctx context.Context, address string) (net.Conn, error)

func ContextDialerCreate(listener *bufconn.Listener) ContextDialer {
	bufDialer := func(ctx context.Context, address string) (net.Conn, error) {
		return listener.Dial()
	}
	return bufDialer
}

func BufDialer(ctx context.Context, address string) (net.Conn, error) {
	return lis.Dial()
}

type certificate struct {
	resourcePath string
	certPEM      string
	certChainPEM []string
}

// CASService is a mock Google CAS Service.
type CASService struct {
	privatecapb.UnimplementedCertificateAuthorityServiceServer
	CertPEM      string
	CertChainPEM []string
	CaCertBundle [][]string
}

func parseCertificateAuthorityPath(p string) (project, location, name string, err error) {
	pieces := strings.Split(p, "/")
	if len(pieces) != 6 {
		return "", "", "", errors.New("malformed certificate authority path")
	}
	if pieces[0] != "projects" {
		return "", "", "", errors.New("malformed certificate authority path")
	}
	project = pieces[1]
	if pieces[2] != "locations" {
		return "", "", "", errors.New("malformed certificate authority path")
	}
	location = pieces[3]
	if pieces[4] != "caPools" {
		return "", "", "", errors.New("malformed certificate authority path")
	}
	name = pieces[5]
	return project, location, name, nil
}

func (ca CASService) certEncode(cert *certificate) *privatecapb.Certificate {
	pb := &privatecapb.Certificate{
		Name: cert.resourcePath,
	}
	if len(cert.certPEM) != 0 {
		pb.PemCertificate = cert.certPEM
	}
	if len(cert.certChainPEM) != 0 {
		pb.PemCertificateChain = cert.certChainPEM
	}
	return pb
}

// CreateCertificate is a mocked function for the Google CAS CA API.
func (ca CASService) CreateCertificate(ctx context.Context, req *privatecapb.CreateCertificateRequest) (*privatecapb.Certificate, error) {
	_, _, _, err := parseCertificateAuthorityPath(req.Parent)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "malformed ca path")
	}
	project, location, authority, _ := parseCertificateAuthorityPath(req.GetParent())
	switch req.GetCertificate().CertificateConfig.(type) {
	case *privatecapb.Certificate_PemCsr:
		return nil, status.Errorf(codes.InvalidArgument, "cannot request certificates using PEM CSR format")
	}
	certResourcePath := path.Join("projects", project, "locations", location, "caPools", authority, "certificates", req.GetCertificate().GetName())
	certObj := &certificate{
		resourcePath: certResourcePath,
		certPEM:      ca.CertPEM,
		certChainPEM: ca.CertChainPEM,
	}
	return ca.certEncode(certObj), nil
}

func (ca CASService) FetchCaCerts(ctx context.Context, req *privatecapb.FetchCaCertsRequest) (*privatecapb.FetchCaCertsResponse, error) {
	_, _, _, err := parseCertificateAuthorityPath(req.GetCaPool())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "malformed ca path")
	}
	certChains := []*privatecapb.FetchCaCertsResponse_CertChain{}
	for _, trustBundle := range ca.CaCertBundle {
		certChain := &privatecapb.FetchCaCertsResponse_CertChain{}
		certChain.Certificates = trustBundle
		certChains = append(certChains, certChain)
	}
	resp := &privatecapb.FetchCaCertsResponse{
		CaCerts: certChains,
	}
	return resp, nil
}

// CASServer is the mocked Google CAS server.
type CASServer struct {
	Server  *grpc.Server
	Address string
}

// CreateServer creates a mocked local Google CAS server and runs it in a separate goroutine.
func CreateServer(service *CASService) (*CASServer, *bufconn.Listener, error) {
	var err error
	s := &CASServer{
		Server: grpc.NewServer(),
	}

	lis = bufconn.Listen(bufSize)
	serveErr := make(chan error, 1)

	go func() {
		privatecapb.RegisterCertificateAuthorityServiceServer(s.Server, service)
		err := s.Server.Serve(lis)
		serveErr <- err
		close(serveErr)
	}()

	select {
	case <-time.After(100 * time.Millisecond):
		err = nil
	case err = <-serveErr:
	}

	if err != nil {
		return nil, nil, err
	}

	return s, lis, nil
}

// Stop stops the Mock Mesh CA server.
func (s *CASServer) Stop() {
	if s.Server != nil {
		s.Server.Stop()
	}
}
