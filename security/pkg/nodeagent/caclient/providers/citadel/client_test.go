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
	"crypto/tls"
	"fmt"
	"net"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "istio.io/api/security/v1alpha1"
	testutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/security/pkg/credentialfetcher/plugin"
	"istio.io/istio/security/pkg/monitoring"
	"istio.io/istio/security/pkg/nodeagent/util"
	ca2 "istio.io/istio/security/pkg/server/ca"
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
	pb.UnimplementedIstioCertificateServiceServer
	Certs         []string
	Authenticator *security.FakeAuthenticator
	Err           error
}

func (ca *mockCAServer) CreateCertificate(ctx context.Context, in *pb.IstioCertificateRequest) (*pb.IstioCertificateResponse, error) {
	if ca.Authenticator != nil {
		caller := ca2.Authenticate(ctx, []security.Authenticator{ca.Authenticator})
		if caller == nil {
			return nil, status.Error(codes.Unauthenticated, "request authenticate failure")
		}
	}
	if ca.Err == nil {
		return &pb.IstioCertificateResponse{CertChain: ca.Certs}, nil
	}
	return nil, ca.Err
}

func tlsOptions(t *testing.T) grpc.ServerOption {
	t.Helper()
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/cert-chain.pem"),
		filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	peerCertVerifier := spiffe.NewPeerCertVerifier()
	if err := peerCertVerifier.AddMappingFromPEM("cluster.local",
		testutil.ReadFile(t, filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/root-cert.pem"))); err != nil {
		t.Fatal(err)
	}
	return grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    peerCertVerifier.GetGeneralCertPool(),
	}))
}

func serve(t *testing.T, ca mockCAServer, opts ...grpc.ServerOption) string {
	// create a local grpc server
	s := grpc.NewServer(opts...)
	t.Cleanup(s.Stop)
	lis, err := net.Listen("tcp", mockServerAddress)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	go func() {
		pb.RegisterIstioCertificateServiceServer(s, &ca)
		if err := s.Serve(lis); err != nil {
			t.Logf("failed to serve: %v", err)
		}
	}()
	_, port, _ := net.SplitHostPort(lis.Addr().String())
	return fmt.Sprintf("localhost:%s", port)
}

func TestCitadelClientRotation(t *testing.T) {
	checkSign := func(t *testing.T, cli security.Client, expectError bool) {
		t.Helper()
		resp, err := cli.CSRSign([]byte{0o1}, 1)
		if expectError != (err != nil) {
			t.Fatalf("expected error:%v, got error:%v", expectError, err)
		}
		if !expectError && !reflect.DeepEqual(resp, fakeCert) {
			t.Fatalf("expected cert: %v", resp)
		}
	}
	certDir := filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot")
	t.Run("cert always present", func(t *testing.T) {
		server := mockCAServer{Certs: fakeCert, Err: nil, Authenticator: security.NewFakeAuthenticator("ca")}
		addr := serve(t, server, tlsOptions(t))
		opts := &security.Options{
			CAEndpoint:  addr,
			CredFetcher: plugin.CreateTokenPlugin("testdata/token"),
			ProvCert:    certDir,
		}
		rootCert := path.Join(certDir, constants.RootCertFilename)
		key := path.Join(certDir, constants.KeyFilename)
		cert := path.Join(certDir, constants.CertChainFilename)
		tlsOpts := &TLSOptions{
			RootCert: rootCert,
			Key:      key,
			Cert:     cert,
		}
		cli, err := NewCitadelClient(opts, tlsOpts)
		if err != nil {
			t.Errorf("failed to create ca client: %v", err)
		}
		t.Cleanup(cli.Close)
		server.Authenticator.Set("fake", "")
		checkSign(t, cli, false)
		// Expiring the token is harder, so just switch to only allow certs
		server.Authenticator.Set("", "istiod.istio-system.svc")
		checkSign(t, cli, false)
		checkSign(t, cli, false)
	})
	t.Run("cert never present", func(t *testing.T) {
		server := mockCAServer{Certs: fakeCert, Err: nil, Authenticator: security.NewFakeAuthenticator("ca")}
		addr := serve(t, server, tlsOptions(t))
		opts := &security.Options{
			CAEndpoint:  addr,
			CredFetcher: plugin.CreateTokenPlugin("testdata/token"),
			ProvCert:    ".",
		}
		rootCert := path.Join(certDir, constants.RootCertFilename)
		key := path.Join(opts.ProvCert, constants.KeyFilename)
		cert := path.Join(opts.ProvCert, constants.CertChainFilename)
		tlsOpts := &TLSOptions{
			RootCert: rootCert,
			Key:      key,
			Cert:     cert,
		}
		cli, err := NewCitadelClient(opts, tlsOpts)
		if err != nil {
			t.Errorf("failed to create ca client: %v", err)
		}
		t.Cleanup(cli.Close)
		server.Authenticator.Set("fake", "")
		checkSign(t, cli, false)
		server.Authenticator.Set("", "istiod.istio-system.svc")
		checkSign(t, cli, true)
	})
	t.Run("cert present later", func(t *testing.T) {
		dir := t.TempDir()
		server := mockCAServer{Certs: fakeCert, Err: nil, Authenticator: security.NewFakeAuthenticator("ca")}
		addr := serve(t, server, tlsOptions(t))
		opts := &security.Options{
			CAEndpoint:  addr,
			CredFetcher: plugin.CreateTokenPlugin("testdata/token"),
			ProvCert:    dir,
		}
		rootCert := path.Join(certDir, constants.RootCertFilename)
		key := path.Join(opts.ProvCert, constants.KeyFilename)
		cert := path.Join(opts.ProvCert, constants.CertChainFilename)
		tlsOpts := &TLSOptions{
			RootCert: rootCert,
			Key:      key,
			Cert:     cert,
		}
		cli, err := NewCitadelClient(opts, tlsOpts)
		if err != nil {
			t.Errorf("failed to create ca client: %v", err)
		}
		t.Cleanup(cli.Close)
		server.Authenticator.Set("fake", "")
		checkSign(t, cli, false)
		checkSign(t, cli, false)
		server.Authenticator.Set("", "istiod.istio-system.svc")
		checkSign(t, cli, true)
		if err := file.Copy(filepath.Join(certDir, "cert-chain.pem"), dir, "cert-chain.pem"); err != nil {
			t.Fatal(err)
		}
		if err := file.Copy(filepath.Join(certDir, "key.pem"), dir, "key.pem"); err != nil {
			t.Fatal(err)
		}
		checkSign(t, cli, false)
	})
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
			expectedErr:  "invalid empty CertChain",
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
			addr := serve(t, tc.server)
			cli, err := NewCitadelClient(&security.Options{CAEndpoint: addr}, nil)
			if err != nil {
				t.Errorf("failed to create ca client: %v", err)
			}
			t.Cleanup(cli.Close)

			resp, err := cli.CSRSign([]byte{0o1}, 1)
			if err != nil {
				if !strings.Contains(err.Error(), tc.expectedErr) {
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
	pb.UnimplementedIstioCertificateServiceServer
	Certs []string
}

func (ca *mockTokenCAServer) CreateCertificate(ctx context.Context, in *pb.IstioCertificateRequest) (*pb.IstioCertificateResponse, error) {
	targetJWT, err := extractBearerToken(ctx)
	if err != nil {
		return nil, err
	}
	if targetJWT != validToken {
		return nil, fmt.Errorf("token is not valid, wanted %q got %q", validToken, targetJWT)
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
				t.Fatalf("failed to listen: %v", err)
			}
			go func() {
				pb.RegisterIstioCertificateServiceServer(s, &tc.server)
				if err := s.Serve(lis); err != nil {
					t.Logf("failed to serve: %v", err)
				}
			}()

			opts := &security.Options{CAEndpoint: lis.Addr().String(), ClusterID: "Kubernetes", CredFetcher: plugin.CreateMockPlugin(tc.token)}
			err = retry.UntilSuccess(func() error {
				cli, err := NewCitadelClient(opts, nil)
				if err != nil {
					return fmt.Errorf("failed to create ca client: %v", err)
				}
				t.Cleanup(cli.Close)
				resp, err := cli.CSRSign([]byte{0o1}, 1)
				if err != nil {
					if !strings.Contains(err.Error(), tc.expectedErr) {
						return fmt.Errorf("error (%s) does not match expected error (%s)", err.Error(), tc.expectedErr)
					}
				} else {
					if tc.expectedErr != "" {
						return fmt.Errorf("expect error: %s but got no error", tc.expectedErr)
					} else if !reflect.DeepEqual(resp, tc.expectedCert) {
						return fmt.Errorf("resp: got %+v, expected %v", resp, tc.expectedCert)
					}
				}
				return nil
			}, retry.Timeout(2*time.Second), retry.Delay(time.Millisecond))
			if err != nil {
				t.Fatalf("test failed error is： %+v", err)
			}
		})
	}
}

func TestCertExpired(t *testing.T) {
	testCases := map[string]struct {
		filepath string
		expected bool
	}{
		"Expired Cert": {
			filepath: "./testdata/expired-cert.pem",
			expected: true,
		},
		"Not Expired Cert": {
			filepath: "./testdata/notexpired-cert.pem",
			expected: false,
		},
	}
	for id, tc := range testCases {

		var wg sync.WaitGroup
		wg.Add(1)
		s := grpc.NewServer()
		defer func() {
			s.Stop()
			wg.Wait()
		}()

		lis, err := net.Listen("tcp", mockServerAddress)
		if err != nil {
			t.Fatalf("failed to listen: %v", err)
		}

		go func() {
			defer wg.Done()
			pb.RegisterIstioCertificateServiceServer(s, &mockTokenCAServer{Certs: []string{}})
			if err := s.Serve(lis); err != nil {
				t.Logf("failed to serve: %v", err)
			}
		}()

		opts := &security.Options{CAEndpoint: lis.Addr().String(), ClusterID: "Kubernetes", CredFetcher: plugin.CreateMockPlugin(validToken)}
		cli, err := NewCitadelClient(opts, nil)
		if err != nil {
			t.Fatalf("failed to create ca client: %v", err)
		}

		t.Cleanup(cli.Close)
		t.Run(id, func(t *testing.T) {
			certExpired, err := cli.isCertExpired(tc.filepath)
			if err != nil {
				t.Fatalf("failed to check the cert, err is: %v", err)
			}
			if certExpired != tc.expected {
				t.Errorf("isCertExpired: get %v, want %v", certExpired, tc.expected)
			}
		})
	}
}
