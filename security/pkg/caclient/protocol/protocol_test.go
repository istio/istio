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

package protocol

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gogo/googleapis/google/rpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"istio.io/istio/security/pkg/pki/util"
	pb "istio.io/istio/security/proto"
)

type FakeIstioCAGrpcServer struct {
	IsApproved bool
	Status     *rpc.Status
	SignedCert []byte
	CertChain  []byte

	response *pb.CsrResponse
	errorMsg string
}

func (s *FakeIstioCAGrpcServer) SetResponseAndError(response *pb.CsrResponse, errorMsg string) {
	s.response = response
	s.errorMsg = errorMsg
}

func (s *FakeIstioCAGrpcServer) HandleCSR(ctx context.Context, req *pb.CsrRequest) (*pb.CsrResponse, error) {
	if len(s.errorMsg) > 0 {
		return nil, fmt.Errorf(s.errorMsg)
	}

	return s.response, nil
}

func TestSendCSRAgainstLocalInstance(t *testing.T) {
	// create a local grpc server
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Errorf("failed to listen: %v", err)
	}
	serv := FakeIstioCAGrpcServer{}

	go func() {
		defer func() {
			s.Stop()
		}()
		pb.RegisterIstioCAServiceServer(s, &serv)
		reflection.Register(s)
		if err := s.Serve(lis); err != nil {
			t.Errorf("failed to serve: %v", err)
		}
	}()

	// The goroutine starting the server may not be ready, results in flakiness.
	time.Sleep(1 * time.Second)

	defaultServerResponse := pb.CsrResponse{
		IsApproved: true,
		Status:     &rpc.Status{Code: int32(rpc.OK), Message: "OK"},
		SignedCert: nil,
		CertChain:  nil,
	}

	testCases := map[string]struct {
		caAddress   string
		dialOptions []grpc.DialOption
		expectedErr string
	}{
		"IstioCAAddress is empty": {
			caAddress: "",
			dialOptions: []grpc.DialOption{
				grpc.WithInsecure()},
			expectedErr: "istio CA address is empty",
		},
		"IstioCAAddress is incorrect": {
			caAddress:   lis.Addr().String() + "1",
			dialOptions: []grpc.DialOption{grpc.WithInsecure()},
			expectedErr: "rpc error: code = Unavailable desc = all SubConns are in TransientFailure",
		},
		"Without Insecure option": {
			caAddress:   lis.Addr().String(),
			dialOptions: []grpc.DialOption{},
			expectedErr: fmt.Sprintf("failed to dial %s: grpc: no transport security set "+
				"(use grpc.WithInsecure() explicitly or set credentials)", lis.Addr().String()),
		},
		"SendCSR not approved": {
			caAddress:   lis.Addr().String(),
			dialOptions: []grpc.DialOption{grpc.WithInsecure()},
			expectedErr: "",
		},
	}

	for id, c := range testCases {
		csr, _, err := util.GenCSR(util.CertOptions{
			Host: "service1",
			Org:  "orgA",
		})
		if err != nil {
			t.Errorf("CSR generation failure (%v)", err)
		}

		req := &pb.CsrRequest{
			CsrPem:              csr,
			CredentialType:      "onprem",
			RequestedTtlMinutes: 60,
		}

		serv.SetResponseAndError(&defaultServerResponse, "")

		grpcClient, err := NewGrpcConnection(c.caAddress, c.dialOptions)
		if err == nil {
			_, err = grpcClient.SendCSR(req)
		}
		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("Error expected: %v", c.expectedErr)
			} else if !strings.Contains(err.Error(), c.expectedErr) {
				t.Errorf("%s: incorrect error message: got [%s] VS want [%s]", id, err.Error(), c.expectedErr)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected expected: %v", err)
			}
		}
	}
}
