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
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	mockpc "istio.io/istio/security/pkg/platform/mock"
	pb "istio.io/istio/security/proto"
)

type FakeIstioCAGrpcServer struct {
	IsApproved      bool
	Status          *rpc.Status
	SignedCertChain []byte

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
		IsApproved:      true,
		Status:          &rpc.Status{Code: int32(rpc.OK), Message: "OK"},
		SignedCertChain: nil,
	}

	testCases := map[string]struct {
		caAddress   string
		pc          platform.Client
		expectedErr string
	}{
		"IstioCAAddress is empty": {
			caAddress: "",
			pc: mockpc.FakeClient{[]grpc.DialOption{
				grpc.WithInsecure(),
			}, "", "service1", "", []byte{}, "", true},
			expectedErr: "istio CA address is empty",
		},
		"IstioCAAddress is incorrect": {
			caAddress: lis.Addr().String() + "1",
			pc: mockpc.FakeClient{[]grpc.DialOption{
				grpc.WithInsecure(),
			}, "", "service1", "", []byte{}, "", true},
			expectedErr: "CSR request failed rpc error: code = Unavailable desc = all SubConns are in TransientFailure",
		},
		"Without Insecure option": {
			caAddress: lis.Addr().String(),
			pc:        mockpc.FakeClient{[]grpc.DialOption{}, "", "service1", "", []byte{}, "", true},
			expectedErr: fmt.Sprintf("failed to dial %s: grpc: no transport security set "+
				"(use grpc.WithInsecure() explicitly or set credentials)", lis.Addr().String()),
		},
		"Error from GetDialOptions": {
			caAddress: lis.Addr().String(),
			pc: mockpc.FakeClient{[]grpc.DialOption{
				grpc.WithInsecure(),
			}, "Error from GetDialOptions", "service1", "", []byte{}, "", true},
			expectedErr: "Error from GetDialOptions",
		},
		"SendCSR not approved": {
			caAddress: lis.Addr().String(),
			pc: mockpc.FakeClient{[]grpc.DialOption{
				grpc.WithInsecure(),
			}, "", "service1", "", []byte{}, "", true},
			expectedErr: "",
		},
	}

	for id, c := range testCases {
		csr, _, err := util.GenCSR(util.CertOptions{
			Host:       "service1",
			Org:        "orgA",
			RSAKeySize: 512,
		})
		if err != nil {
			t.Errorf("CSR generation failure (%v)", err)
		}

		cred, err := c.pc.GetAgentCredential()
		if err != nil {
			t.Errorf("Error getting credential (%v)", err)
		}

		req := &pb.CsrRequest{
			CsrPem:              csr,
			NodeAgentCredential: cred,
			CredentialType:      c.pc.GetCredentialType(),
			RequestedTtlMinutes: 60,
		}

		serv.SetResponseAndError(&defaultServerResponse, "")

		client := &CAGrpcClientImpl{}
		_, err = client.SendCSR(req, c.pc, c.caAddress)
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
