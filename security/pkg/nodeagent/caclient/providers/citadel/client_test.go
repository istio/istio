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

package caclient

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"istio.io/istio/pkg/jwt"
	citadelca "istio.io/istio/security/pkg/server/ca"
	"istio.io/istio/security/pkg/pki/ca"
	"k8s.io/client-go/kubernetes/fake"

	pb "istio.io/istio/security/proto"
)

const mockServerAddress = "localhost:0"

var (
	fakeCert  = []string{"foo", "bar"}
	fakeToken = "Bearer fakeToken"
)

type mockCAServer struct {
	Certs []string
	Err   error
}

func (ca *mockCAServer) CreateCertificate(ctx context.Context, in *pb.IstioCertificateRequest) (*pb.IstioCertificateResponse, error) {
	if ca.Err == nil {
		return &pb.IstioCertificateResponse{CertChain: ca.Certs}, nil
	}
	return nil, ca.Err
}

func TestE2EClient(t *testing.T) {
	cases := map[string]struct {
		rootCertFile    string
		certChainFile   string
		signingCertFile string
		signingKeyFile  string
	}{
		"RSA server cert": {
			rootCertFile:    "../../../../pki/testdata/multilevelpki/root-cert.pem",
			certChainFile:   "../../../../pki/testdata/multilevelpki/int2-cert-chain.pem",
			signingCertFile: "../../../../pki/testdata/multilevelpki/int2-cert.pem",
			signingKeyFile:  "../../../../pki/testdata/multilevelpki/int2-key.pem",
		},
		"ECC server cert": {
			rootCertFile:    "../../../../pki/testdata/multilevelpki/ecc-root-cert.pem",
			certChainFile:   "../../../../pki/testdata/multilevelpki/ecc-int2-cert-chain.pem",
			signingCertFile: "../../../../pki/testdata/multilevelpki/ecc-int2-cert.pem",
			signingKeyFile:  "../../../../pki/testdata/multilevelpki/ecc-int2-key.pem",
		},
	}
	for id, tc := range cases {
		client := fake.NewSimpleClientset()
		caNamespace := "default"
		defaultWorkloadCertTTL := 30 * time.Minute
		maxWorkloadCertTTL := time.Hour

		caopts, err := ca.NewPluggedCertIstioCAOptions(tc.certChainFile, tc.signingCertFile, tc.signingKeyFile, tc.rootCertFile,
			defaultWorkloadCertTTL, maxWorkloadCertTTL, caNamespace, client.CoreV1())
		if err != nil {
			t.Fatalf("%s: Failed to create a plugged-cert CA Options: %v", id, err)
		}

		ca, err := ca.NewIstioCA(caopts)
		if err != nil {
			t.Errorf("%s: Got error while creating plugged-cert CA: %v", id, err)
		}
		if ca == nil {
			t.Fatalf("Failed to create a plugged-cert CA.")
		}

		server, createServerErr := citadelca.New(ca, time.Hour, false, []string{"localhost"}, 0,
			"testdomain.com", true, jwt.PolicyThirdParty, "kubernetes")

		if err != nil {
			t.Errorf("%s: Cannot create server: %v", id, createServerErr)
		}

		s := grpc.NewServer()
		defer s.Stop()
		lis, err := net.Listen("tcp", mockServerAddress)
		if err != nil {
			t.Fatalf("Test case [%s]: failed to listen: %v", id, err)
		}

		go func() {
			pb.RegisterIstioCertificateServiceServer(s, server)
			if err := s.Serve(lis); err != nil {
				t.Logf("Test case [%s]: failed to serve: %v", id, err)
			}
		}()

		//request := &pb.IstioCertificateRequest{Csr: "dumb CSR"}
		//_, createErr := server.CreateCertificate(context.Background(), request)
		//if createErr != nil {
		//	t.Errorf("%s: getServerCertificate error: %v", id, createErr)
		//}

		// The goroutine starting the server may not be ready, results in flakiness.
		time.Sleep(1 * time.Second)

		cli, err := NewCitadelClient(lis.Addr().String(), false, nil, "")
		if err != nil {
			t.Errorf("Test case [%s]: failed to create ca client: %v", id, err)
		}
		resp, err := cli.CSRSign(context.Background(), "12345678-1234-1234-1234-123456789012", []byte{01}, fakeToken, 1)
		if err != nil {
				t.Logf("%+v", resp)
				t.Errorf("Test case [%s]: error (%s) happens ", id, err.Error())
		} else {
			//if !reflect.DeepEqual(resp, tc.expectedCert) {
			//	t.Errorf("Test case [%s]: resp: got %+v, expected %v", id, resp, tc.expectedCert)
			//}
		}
	}

}

//func TestCitadelClient(t *testing.T) {
//	testCases := map[string]struct {
//		server       mockCAServer
//		expectedCert []string
//		expectedErr  string
//	}{
//		"Valid certs": {
//			server:       mockCAServer{Certs: fakeCert, Err: nil},
//			expectedCert: fakeCert,
//			expectedErr:  "",
//		},
//		"Error in response": {
//			server:       mockCAServer{Certs: nil, Err: fmt.Errorf("test failure")},
//			expectedCert: nil,
//			expectedErr:  "rpc error: code = Unknown desc = test failure",
//		},
//		"Empty response": {
//			server:       mockCAServer{Certs: []string{}, Err: nil},
//			expectedCert: nil,
//			expectedErr:  "invalid response cert chain",
//		},
//	}
//
//	for id, tc := range testCases {
//		// create a local grpc server
//		s := grpc.NewServer()
//		defer s.Stop()
//		lis, err := net.Listen("tcp", mockServerAddress)
//		if err != nil {
//			t.Fatalf("Test case [%s]: failed to listen: %v", id, err)
//		}
//
//		go func() {
//			pb.RegisterIstioCertificateServiceServer(s, &tc.server)
//			if err := s.Serve(lis); err != nil {
//				t.Logf("Test case [%s]: failed to serve: %v", id, err)
//			}
//		}()
//
//		// The goroutine starting the server may not be ready, results in flakiness.
//		time.Sleep(1 * time.Second)
//
//		cli, err := NewCitadelClient(lis.Addr().String(), false, nil, "")
//		if err != nil {
//			t.Errorf("Test case [%s]: failed to create ca client: %v", id, err)
//		}
//		t.Log("TestCitadelClient")
//		t.Log("ssssssssssss")
//		resp, err := cli.CSRSign(context.Background(), "12345678-1234-1234-1234-123456789012", []byte{01}, fakeToken, 1)
//		if err != nil {
//			if err.Error() != tc.expectedErr {
//				t.Errorf("Test case [%s]: error (%s) does not match expected error (%s)", id, err.Error(), tc.expectedErr)
//			}
//		} else {
//			if tc.expectedErr != "" {
//				t.Errorf("Test case [%s]: expect error: %s but got no error", id, tc.expectedErr)
//			} else if !reflect.DeepEqual(resp, tc.expectedCert) {
//				t.Errorf("Test case [%s]: resp: got %+v, expected %v", id, resp, tc.expectedCert)
//			}
//		}
//	}
//}
