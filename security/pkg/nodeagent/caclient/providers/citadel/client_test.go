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
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/security/pkg/monitoring"
	"istio.io/istio/security/pkg/nodeagent/util"
	"istio.io/istio/tests/util/leak"
)

const (
	mockServerAddress = "localhost:0"
)

var (
	fakeCert          = []string{"foo", "bar"}
	fakeToken         = "Bearer fakeToken"
	validToken        = "Bearer validToken"
	authorizationMeta = "authorization"
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

func TestCitadelClient(t *testing.T) {
	testCases := map[string]struct {
		server       mockCAServer
		expectedCert []string
		expectedErr  string
		expectRetry  bool
	}{
		"Valid certs": {
			server:       mockCAServer{Certs: fakeCert, Err: nil},
			expectedCert: fakeCert,
			expectedErr:  "",
		},
		"Error in response": {
			server:       mockCAServer{Certs: nil, Err: fmt.Errorf("test failure")},
			expectedCert: nil,
			expectedErr:  "rpc error: code = Unknown desc = test failure",
		},
		"Empty response": {
			server:       mockCAServer{Certs: []string{}, Err: nil},
			expectedCert: nil,
			expectedErr:  "invalid response cert chain",
		},
		"retry": {
			server:       mockCAServer{Certs: nil, Err: status.Error(codes.Unavailable, "test failure")},
			expectedCert: nil,
			expectedErr:  "rpc error: code = Unavailable desc = test failure",
			expectRetry:  true,
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			monitoring.Reset()
			// create a local grpc server
			s := grpc.NewServer()
			t.Cleanup(s.Stop)
			lis, err := net.Listen("tcp", mockServerAddress)
			if err != nil {
				t.Fatalf("failed to listen: %v", err)
			}

			go func() {
				pb.RegisterIstioCertificateServiceServer(s, &tc.server)
				if err := s.Serve(lis); err != nil {
					t.Logf("failed to serve: %v", err)
				}
			}()

			cli, err := NewCitadelClient(lis.Addr().String(), false, nil, "")
			if err != nil {
				t.Errorf("failed to create ca client: %v", err)
			}
			t.Cleanup(cli.Close)

			resp, err := cli.CSRSign(context.Background(), []byte{01}, fakeToken, 1)
			if err != nil {
				if err.Error() != tc.expectedErr {
					t.Errorf("error (%s) does not match expected error (%s)", err.Error(), tc.expectedErr)
				}
			} else {
				if tc.expectedErr != "" {
					t.Errorf("expect error: %s but got no error", tc.expectedErr)
				} else if !reflect.DeepEqual(resp, tc.expectedCert) {
					t.Errorf("resp: got %+v, expected %v", resp, tc.expectedCert)
				}
			}

			if tc.expectRetry {
				retry.UntilSuccessOrFail(t, func() error {
					g, err := util.GetMetricsCounterValueWithTags("num_outgoing_retries", map[string]string{
						"request_type": monitoring.CSR,
					})
					if err != nil {
						return err
					}
					if g <= 0 {
						return fmt.Errorf("expected retries, got %v", g)
					}
					return nil
				}, retry.Timeout(time.Second*5))
			}
		})
	}
}

type mockTokenCAServer struct {
	Certs []string
}

func (ca *mockTokenCAServer) CreateCertificate(ctx context.Context, in *pb.IstioCertificateRequest) (*pb.IstioCertificateResponse, error) {
	targetJWT, err := extractBearerToken(ctx)
	if err != nil {
		return nil, err
	}
	if targetJWT != validToken {
		return nil, fmt.Errorf("token is not valid")
	}
	return &pb.IstioCertificateResponse{CertChain: ca.Certs}, nil
}

func extractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", fmt.Errorf("no metadata is attached")
	}

	authHeader, exists := md[authorizationMeta]
	if !exists {
		return "", fmt.Errorf("no HTTP authorization header exists")
	}

	for _, value := range authHeader {
		if strings.HasPrefix(value, bearerTokenPrefix) {
			return strings.TrimPrefix(value, bearerTokenPrefix), nil
		}
	}

	return "", fmt.Errorf("no bearer token exists in HTTP authorization header")
}

// this test is to test whether the server side receive the correct token when
// we build the CSR sign request
func TestCitadelClientWithDifferentTypeToken(t *testing.T) {
	testCases := map[string]struct {
		server       mockTokenCAServer
		expectedCert []string
		expectedErr  string
		token        string
	}{
		"Valid Token": {
			server:       mockTokenCAServer{Certs: fakeCert},
			expectedCert: fakeCert,
			expectedErr:  "",
			token:        validToken,
		},
		"Empty Token": {
			server:       mockTokenCAServer{Certs: nil},
			expectedCert: nil,
			expectedErr:  "rpc error: code = Unknown desc = no HTTP authorization header exists",
			token:        "",
		},
		"InValid Token": {
			server:       mockTokenCAServer{Certs: []string{}},
			expectedCert: nil,
			expectedErr:  "rpc error: code = Unknown desc = token is not valid",
			token:        fakeToken,
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			s := grpc.NewServer()
			defer s.Stop()
			lis, err := net.Listen("tcp", mockServerAddress)
			if err != nil {
				t.Fatalf("test case [%s]: failed to listen: %v", id, err)
			}
			go func() {
				pb.RegisterIstioCertificateServiceServer(s, &tc.server)
				if err := s.Serve(lis); err != nil {
					t.Logf("Test case [%s]: failed to serve: %v", id, err)
				}
			}()

			err = retry.UntilSuccess(func() error {
				cli, err := NewCitadelClient(lis.Addr().String(), false, nil, "Kubernetes")
				if err != nil {
					return fmt.Errorf("test case [%s]: failed to create ca client: %v", id, err)
				}
				t.Cleanup(cli.Close)
				resp, err := cli.CSRSign(context.Background(), []byte{01}, tc.token, 1)
				if err != nil {
					if err.Error() != tc.expectedErr {
						return fmt.Errorf("test case [%s]: error (%s) does not match expected error (%s)", id, err.Error(), tc.expectedErr)
					}
				} else {
					if tc.expectedErr != "" {
						return fmt.Errorf("test case [%s]: expect error: %s but got no error", id, tc.expectedErr)
					} else if !reflect.DeepEqual(resp, tc.expectedCert) {
						return fmt.Errorf("test case [%s]: resp: got %+v, expected %v", id, resp, tc.expectedCert)
					}
				}
				return nil
			}, retry.Timeout(20*time.Second), retry.Delay(2*time.Second))
			if err != nil {
				t.Fatalf("test failed error is： %+v", err)
			}
		})
	}
}

func TestMain(m *testing.M) {
	leak.CheckMain(m)
}
