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

package ca

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/server/ca/authenticate"
)

const (
	testCert = `-----BEGIN CERTIFICATE-----
Y2VydA==
-----END CERTIFICATE-----
`

	testIntermediateCert = `-----BEGIN CERTIFICATE-----
aW50ZXJtZWRpYXRlMQ==
-----END CERTIFICATE-----
`

	testIntermediateCert2 = `-----BEGIN CERTIFICATE-----
aW50ZXJtZWRpYXRlMg==
-----END CERTIFICATE-----
`

	testCertChain = testIntermediateCert // this chain has only one intermediate

	testMultiCertChain = testIntermediateCert + "\n" + testIntermediateCert2 // cert chain with multiple intermediates

	testRootCert = `-----BEGIN CERTIFICATE-----
cm9vdF9jZXJ0
-----END CERTIFICATE-----
`
)

type mockAuthenticator struct {
	authSource     security.AuthSource
	identities     []string
	kubernetesInfo security.KubernetesInfo
	errMsg         string
}

func (authn *mockAuthenticator) AuthenticatorType() string {
	return "mockAuthenticator"
}

func (authn *mockAuthenticator) Authenticate(_ security.AuthContext) (*security.Caller, error) {
	if len(authn.errMsg) > 0 {
		return nil, fmt.Errorf("%v", authn.errMsg)
	}

	return &security.Caller{
		AuthSource:     authn.authSource,
		Identities:     authn.identities,
		KubernetesInfo: authn.kubernetesInfo,
	}, nil
}

type mockAuthInfo struct {
	authType string
}

func (ai mockAuthInfo) AuthType() string {
	return ai.authType
}

/*
This is a testing to send a request to the server using
the client cert authenticator instead of mock authenticator
*/
func TestCreateCertificateE2EUsingClientCertAuthenticator(t *testing.T) {
	callerID := "test.identity"
	ids := []util.Identity{
		{Type: util.TypeURI, Value: []byte(callerID)},
	}
	sanExt, err := util.BuildSANExtension(ids)
	if err != nil {
		t.Error(err)
	}
	auth := &authenticate.ClientCertAuthenticator{}

	server := &Server{
		ca: &mockca.FakeCA{
			SignedCert:    []byte(testCert),
			KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte(testCertChain), []byte(testRootCert), nil),
		},
		Authenticators: []security.Authenticator{auth},
		monitoring:     newMonitoringMetrics(),
	}
	mockCertChain := []string{testCert, testCertChain, testRootCert}
	mockIPAddr := &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)}
	testCerts := map[string]struct {
		certChain    [][]*x509.Certificate
		caller       *security.Caller
		fakeAuthInfo *mockAuthInfo
		code         codes.Code
		ipAddr       *net.IPAddr
	}{
		// no client certificate is presented
		"No client certificate": {
			certChain: nil,
			caller:    nil,
			ipAddr:    mockIPAddr,
			code:      codes.Unauthenticated,
		},
		// "unsupported auth type: not-tls"
		"Unsupported auth type": {
			certChain:    nil,
			caller:       nil,
			fakeAuthInfo: &mockAuthInfo{"not-tls"},
			ipAddr:       mockIPAddr,
			code:         codes.Unauthenticated,
		},
		// no cert chain presented
		"Empty cert chain": {
			certChain: [][]*x509.Certificate{},
			caller:    nil,
			ipAddr:    mockIPAddr,
			code:      codes.Unauthenticated,
		},
		// certificate misses the SAN field
		"Certificate has no SAN": {
			certChain: [][]*x509.Certificate{
				{
					{
						Version: 1,
					},
				},
			},
			ipAddr: mockIPAddr,
			code:   codes.Unauthenticated,
		},
		// successful testcase with valid client certificate
		"With client certificate": {
			certChain: [][]*x509.Certificate{
				{
					{
						Extensions: []pkix.Extension{*sanExt},
					},
				},
			},
			caller: &security.Caller{Identities: []string{callerID}},
			ipAddr: mockIPAddr,
			code:   codes.OK,
		},
	}

	for id, c := range testCerts {
		request := &pb.IstioCertificateRequest{Csr: "dumb CSR"}
		ctx := context.Background()
		if c.certChain != nil {
			tlsInfo := credentials.TLSInfo{
				State: tls.ConnectionState{VerifiedChains: c.certChain},
			}
			p := &peer.Peer{Addr: c.ipAddr, AuthInfo: tlsInfo}
			ctx = peer.NewContext(ctx, p)
		}
		if c.fakeAuthInfo != nil {
			ctx = peer.NewContext(ctx, &peer.Peer{Addr: c.ipAddr, AuthInfo: c.fakeAuthInfo})
		}
		response, err := server.CreateCertificate(ctx, request)

		s, _ := status.FromError(err)
		code := s.Code()
		if code != c.code {
			t.Errorf("Case %s: expecting code to be (%d) but got (%d): %s", id, c.code, code, s.Message())
		} else if c.code == codes.OK {
			if len(response.CertChain) != len(mockCertChain) {
				t.Errorf("Case %s: expecting cert chain length to be (%d) but got (%d)",
					id, len(mockCertChain), len(response.CertChain))
			}
			for i, v := range response.CertChain {
				if v != mockCertChain[i] {
					t.Errorf("Case %s: expecting cert to be (%s) but got (%s) at position [%d] of cert chain.",
						id, mockCertChain, v, i)
				}
			}
		}
	}
}

func TestCreateCertificate(t *testing.T) {
	testCases := map[string]struct {
		authenticators []security.Authenticator
		ca             CertificateAuthority
		certChain      []string
		code           codes.Code
	}{
		"No authenticator": {
			authenticators: nil,
			code:           codes.Unauthenticated,
			ca:             &mockca.FakeCA{},
		},
		"Unauthenticated request": {
			authenticators: []security.Authenticator{&mockAuthenticator{
				errMsg: "Not authorized",
			}},
			code: codes.Unauthenticated,
			ca:   &mockca.FakeCA{},
		},
		"CA not ready": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.CANotReady, fmt.Errorf("cannot sign"))},
			code:           codes.Internal,
		},
		"Invalid CSR": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.CSRError, fmt.Errorf("cannot sign"))},
			code:           codes.InvalidArgument,
		},
		"Invalid TTL": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.TTLError, fmt.Errorf("cannot sign"))},
			code:           codes.InvalidArgument,
		},
		"Failed to sign": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.CertGenError, fmt.Errorf("cannot sign"))},
			code:           codes.Internal,
		},
		"Successful signing": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca: &mockca.FakeCA{
				SignedCert:    []byte(testCert),
				KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte(testCertChain), []byte(testRootCert), nil),
			},
			certChain: []string{testCert, testCertChain, testRootCert},
			code:      codes.OK,
		},
		"Successful signing w/ multi-cert chain": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca: &mockca.FakeCA{
				SignedCert: []byte(testCert),
				KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil,
					[]byte(testMultiCertChain),
					[]byte(testRootCert),
					nil,
				),
			},
			certChain: []string{testCert, testIntermediateCert, testIntermediateCert2, testRootCert}, // the response should have one cert per element in slice
			code:      codes.OK,
		},
	}

	p := &peer.Peer{Addr: &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)}, AuthInfo: credentials.TLSInfo{}}
	ctx := peer.NewContext(context.Background(), p)
	for id, c := range testCases {
		server := &Server{
			ca:             c.ca,
			Authenticators: c.authenticators,
			monitoring:     newMonitoringMetrics(),
		}
		request := &pb.IstioCertificateRequest{Csr: "dumb CSR"}

		response, err := server.CreateCertificate(ctx, request)
		s, _ := status.FromError(err)
		code := s.Code()
		if c.code != code {
			t.Errorf("Case %s: expecting code to be (%d) but got (%d): %s", id, c.code, code, s.Message())
		} else if c.code == codes.OK {
			if len(response.CertChain) != len(c.certChain) {
				t.Errorf("Case %s: expecting cert chain length to be (%d) but got (%d)",
					id, len(c.certChain), len(response.CertChain))
			}
			for i, v := range response.CertChain {
				if v != c.certChain[i] {
					t.Errorf("Case %s: expecting cert to be (%s) but got (%s) at position [%d] of cert chain.",
						id, c.certChain, v, i)
				}
			}

		}
	}
}

func TestCreateCertificateE2EWithImpersonateIdentity(t *testing.T) {
	allowZtunnel := sets.Set[types.NamespacedName]{
		{Name: "ztunnel", Namespace: "istio-system"}: {},
	}
	ztunnelCaller := security.KubernetesInfo{
		PodName:           "ztunnel-a",
		PodNamespace:      "istio-system",
		PodUID:            "12345",
		PodServiceAccount: "ztunnel",
	}
	ztunnelPod := pod{
		name:      ztunnelCaller.PodName,
		namespace: ztunnelCaller.PodNamespace,
		account:   ztunnelCaller.PodServiceAccount,
		uid:       ztunnelCaller.PodUID,
		node:      "zt-node",
	}
	podSameNode := pod{
		name:      "pod-a",
		namespace: "ns-a",
		account:   "sa-a",
		uid:       "1",
		node:      "zt-node",
	}
	podOtherNode := pod{
		name:      "pod-b",
		namespace: podSameNode.namespace,
		account:   podSameNode.account,
		uid:       "2",
		node:      "other-node",
	}

	ztunnelCallerRemote := security.KubernetesInfo{
		PodName:           "ztunnel-b",
		PodNamespace:      "istio-system",
		PodUID:            "12346",
		PodServiceAccount: "ztunnel",
	}
	ztunnelPodRemote := pod{
		name:      ztunnelCallerRemote.PodName,
		namespace: ztunnelCallerRemote.PodNamespace,
		account:   ztunnelCallerRemote.PodServiceAccount,
		uid:       ztunnelCallerRemote.PodUID,
		node:      "zt-node-remote",
	}
	podSameNodeRemote := pod{
		name:      "pod-c",
		namespace: podSameNode.namespace,
		account:   podSameNode.account,
		uid:       "3",
		node:      "zt-node-remote",
	}

	testCases := []struct {
		name                string
		authenticators      []security.Authenticator
		ca                  CertificateAuthority
		certChain           []string
		pods                []pod
		impersonatePod      pod
		callerClusterID     cluster.ID
		trustedNodeAccounts sets.Set[types.NamespacedName]
		isMultiCluster      bool
		remoteClusterPods   []pod
		code                codes.Code
	}{
		{
			name: "No node authorizer",
			authenticators: []security.Authenticator{&mockAuthenticator{
				identities:     []string{"test-identity"},
				kubernetesInfo: ztunnelCaller,
			}},
			ca: &mockca.FakeCA{
				SignedCert:    []byte(testCert),
				KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte(testCertChain), []byte(testRootCert), nil),
			},
			certChain:           []string{testCert, testCertChain, testRootCert},
			trustedNodeAccounts: sets.Set[types.NamespacedName]{},
			code:                codes.Unauthenticated,
		},
		{
			name: "Pod not passing node authorization",
			authenticators: []security.Authenticator{&mockAuthenticator{
				identities:     []string{"test-identity"},
				kubernetesInfo: ztunnelCaller,
			}},
			ca: &mockca.FakeCA{
				SignedCert:    []byte(testCert),
				KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte(testCertChain), []byte(testRootCert), nil),
			},
			certChain:           []string{testCert, testCertChain, testRootCert},
			pods:                []pod{ztunnelPod, podOtherNode},
			impersonatePod:      podOtherNode,
			callerClusterID:     cluster.ID("fake"),
			trustedNodeAccounts: allowZtunnel,
			code:                codes.Unauthenticated,
		},
		{
			name: "Successful signing with impersonate identity",
			authenticators: []security.Authenticator{&mockAuthenticator{
				identities:     []string{"test-identity"},
				kubernetesInfo: ztunnelCaller,
			}},
			ca: &mockca.FakeCA{
				SignedCert:    []byte(testCert),
				KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte(testCertChain), []byte(testRootCert), nil),
			},
			certChain:           []string{testCert, testCertChain, testRootCert},
			pods:                []pod{ztunnelPod, podSameNode},
			impersonatePod:      podSameNode,
			callerClusterID:     cluster.ID("fake"),
			trustedNodeAccounts: allowZtunnel,
			code:                codes.OK,
		},
		{
			name: "Pod not passing node authorization because of ztunnel from other clusters",
			authenticators: []security.Authenticator{&mockAuthenticator{
				identities:     []string{"test-identity"},
				kubernetesInfo: ztunnelCaller,
			}},
			ca: &mockca.FakeCA{
				SignedCert:    []byte(testCert),
				KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte(testCertChain), []byte(testRootCert), nil),
			},
			certChain:           []string{testCert, testCertChain, testRootCert},
			pods:                []pod{ztunnelPod},
			impersonatePod:      podSameNodeRemote,
			callerClusterID:     cluster.ID("fake"),
			trustedNodeAccounts: allowZtunnel,
			isMultiCluster:      true,
			remoteClusterPods:   []pod{ztunnelPodRemote, podSameNodeRemote},
			code:                codes.Unauthenticated,
		},
		{
			name: "Successful signing with impersonate identity from remote cluster",
			authenticators: []security.Authenticator{&mockAuthenticator{
				identities:     []string{"test-identity"},
				kubernetesInfo: ztunnelCallerRemote,
			}},
			ca: &mockca.FakeCA{
				SignedCert:    []byte(testCert),
				KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte(testCertChain), []byte(testRootCert), nil),
			},
			certChain:           []string{testCert, testCertChain, testRootCert},
			pods:                []pod{ztunnelPod, podSameNode},
			impersonatePod:      podSameNodeRemote,
			callerClusterID:     cluster.ID("fake-remote"),
			trustedNodeAccounts: allowZtunnel,
			isMultiCluster:      true,
			remoteClusterPods:   []pod{ztunnelPodRemote, podSameNodeRemote},
			code:                codes.OK,
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			test.SetForTest(t, &features.CATrustedNodeAccounts, c.trustedNodeAccounts)

			multiClusterController := multicluster.NewFakeController()
			server, _ := New(c.ca, time.Duration(1), c.authenticators, multiClusterController)

			var pods []runtime.Object
			for _, p := range c.pods {
				pods = append(pods, toPod(p, strings.HasPrefix(p.name, "ztunnel")))
			}
			client := kube.NewFakeClient(pods...)
			stop := test.NewStop(t)
			multiClusterController.Add("fake", client, stop)
			client.RunAndWait(stop)

			if c.isMultiCluster {
				var remoteClusterPods []runtime.Object
				for _, p := range c.remoteClusterPods {
					remoteClusterPods = append(remoteClusterPods, toPod(p, strings.HasPrefix(p.name, "ztunnel")))
				}
				remoteClient := kube.NewFakeClient(remoteClusterPods...)
				multiClusterController.Add("fake-remote", remoteClient, stop)
				remoteClient.RunAndWait(stop)
			}

			if server.nodeAuthorizer != nil {
				for _, c := range server.nodeAuthorizer.component.All() {
					kube.WaitForCacheSync("test", stop, c.pods.HasSynced)
				}
			}

			reqMeta, _ := structpb.NewStruct(map[string]any{
				security.ImpersonatedIdentity: c.impersonatePod.Identity(),
			})
			request := &pb.IstioCertificateRequest{
				Csr:      "dumb CSR",
				Metadata: reqMeta,
			}

			p := &peer.Peer{Addr: &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)}, AuthInfo: credentials.TLSInfo{}}
			ctx := peer.NewContext(context.Background(), p)
			if c.callerClusterID != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.MD{
					"clusterid": []string{string(c.callerClusterID)},
				})
			}

			response, err := server.CreateCertificate(ctx, request)
			s, _ := status.FromError(err)
			code := s.Code()
			if c.code != code {
				t.Errorf("Case %s: expecting code to be (%d) but got (%d): %s", c.name, c.code, code, s.Message())
			} else if c.code == codes.OK {
				if len(response.CertChain) != len(c.certChain) {
					t.Errorf("Case %s: expecting cert chain length to be (%d) but got (%d)",
						c.name, len(c.certChain), len(response.CertChain))
				}
				for i, v := range response.CertChain {
					if v != c.certChain[i] {
						t.Errorf("Case %s: expecting cert to be (%s) but got (%s) at position [%d] of cert chain.",
							c.name, c.certChain, v, i)
					}
				}

			}
		})
	}
}

func TestRecordCertsExipryMetrics(t *testing.T) {
	t.Run("test certificate metric recording", func(t *testing.T) {
		mt := monitortest.New(t)
		now := time.Now()
		rootTTL := time.Hour
		certTTL := time.Hour / 2
		rootCertBytes, rootKeyBytes, err := util.GenCertKeyFromOptions(util.CertOptions{
			Host:         "citadel.testing.istio.io",
			Org:          "MyOrg",
			NotBefore:    now,
			IsCA:         true,
			IsSelfSigned: true,
			TTL:          rootTTL,
			RSAKeySize:   2048,
		})
		if err != nil {
			t.Errorf("failed to gen root cert for Citadel self signed cert %v", err)
		}

		rootCert, err := util.ParsePemEncodedCertificate(rootCertBytes)
		if err != nil {
			t.Errorf("failed to parse pem %v", err)
		}

		rootKey, err := util.ParsePemEncodedKey(rootKeyBytes)
		if err != nil {
			t.Errorf("failed to parse pem %v", err)
		}

		caCertBytes, caCertKeyBytes, err := util.GenCertKeyFromOptions(util.CertOptions{
			Host:         "citadel.testing.istio.io",
			Org:          "MyOrg",
			NotBefore:    now,
			TTL:          certTTL,
			IsServer:     true,
			IsCA:         true,
			IsSelfSigned: false,
			RSAKeySize:   2048,
			SignerCert:   rootCert,
			SignerPriv:   rootKey,
		})
		if err != nil {
			t.Fatalf("failed to gen CA cert %v", err)
		}

		kb := util.NewKeyCertBundleFromPem(
			caCertBytes, caCertKeyBytes, caCertBytes, rootCertBytes, nil,
		)

		RecordCertsExpiry(kb)

		mt.Assert(rootCertExpiryTimestamp.Name(), nil, monitortest.AlmostEquals(float64(now.Add(rootTTL).Unix()), 1e-7))
		mt.Assert(certChainExpiryTimestamp.Name(), nil, monitortest.AlmostEquals(float64(now.Add(certTTL).Unix()), 1e-7))
		eps := float64(time.Minute) // so the tests aren't flaky
		mt.Assert(rootCertExpirySeconds.Name(), nil, monitortest.AlmostEquals(rootTTL.Seconds(), eps))
		mt.Assert(certChainExpirySeconds.Name(), nil, monitortest.AlmostEquals(certTTL.Seconds(), eps))

		now = time.Now()
		rootTTL = time.Hour * 60
		certTTL = time.Hour * 20
		rootCertBytes, rootKeyBytes, err = util.GenCertKeyFromOptions(util.CertOptions{
			Host:         "other-citadel.testing.istio.io",
			Org:          "other-MyOrg",
			NotBefore:    now,
			IsCA:         true,
			IsSelfSigned: true,
			TTL:          rootTTL,
			RSAKeySize:   2048,
		})
		if err != nil {
			t.Errorf("failed to gen root cert %v", err)
		}

		rootCert, err = util.ParsePemEncodedCertificate(rootCertBytes)
		if err != nil {
			t.Errorf("failed to parse pem %v", err)
		}

		rootKey, err = util.ParsePemEncodedKey(rootKeyBytes)
		if err != nil {
			t.Errorf("failed to parse pem %v", err)
		}

		caCertBytes, caCertKeyBytes, err = util.GenCertKeyFromOptions(util.CertOptions{
			Host:         "other-citadel.testing.istio.io",
			Org:          "other-MyOrg",
			NotBefore:    now,
			TTL:          certTTL,
			IsServer:     true,
			IsCA:         true,
			IsSelfSigned: false,
			RSAKeySize:   2048,
			SignerCert:   rootCert,
			SignerPriv:   rootKey,
		})
		if err != nil {
			t.Fatalf("failed to gen CA cert %v", err)
		}

		kb = util.NewKeyCertBundleFromPem(
			caCertBytes, caCertKeyBytes, caCertBytes, rootCertBytes, nil,
		)

		RecordCertsExpiry(kb)

		mt.Assert(rootCertExpiryTimestamp.Name(), nil, monitortest.AlmostEquals(float64(now.Add(rootTTL).Unix()), 1e-7))
		mt.Assert(certChainExpiryTimestamp.Name(), nil, monitortest.AlmostEquals(float64(now.Add(certTTL).Unix()), 1e-7))
		mt.Assert(rootCertExpirySeconds.Name(), nil, monitortest.AlmostEquals(rootTTL.Seconds(), eps))
		mt.Assert(certChainExpirySeconds.Name(), nil, monitortest.AlmostEquals(certTTL.Seconds(), eps))
	})
}
