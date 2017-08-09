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

package grpc

import (
	"github.com/golang/glog"

	"golang.org/x/net/context"

	"istio.io/auth/pkg/pki"
	"istio.io/auth/pkg/pki/ca"
	pb "istio.io/auth/proto"
)

type server struct {
	ca ca.CertificateAuthority
}

func (s *server) HandleCSR(ctx context.Context, request *pb.Request) (*pb.Response, error) {
	// TODO: handle authentication here

	csr, err := pki.ParsePemEncodedCSR(request.CsrPem)
	if err != nil {
		glog.Error(err)
		// TODO: possibly wrap err in GRPC error
		return nil, err
	}

	cert, err := s.ca.Sign(csr)
	if err != nil {
		glog.Error(err)
		// TODO: possibly wrap err in GRPC error
		return nil, err
	}

	response := &pb.Response{
		IsApproved:      true,
		SignedCertChain: cert,
	}

	return response, nil
}

// New creates a new instance of `IstioCAServiceServer`.
func New(ca ca.CertificateAuthority) pb.IstioCAServiceServer {
	return &server{ca}
}
