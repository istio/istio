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

package xds_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	credentials "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
	xdsserver "istio.io/istio/pkg/xds"
)

func makeSecret(name string, data map[string]string) *corev1.Secret {
	bdata := map[string][]byte{}
	for k, v := range data {
		bdata[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "istio-system",
		},
		Data: bdata,
	}
}

var (
	certDir      = filepath.Join(env.IstioSrc, "./tests/testdata/certs")
	simpleCaCert = makeSecret("ca-only", map[string]string{
		credentials.GenericScrtCaCert: readFile(filepath.Join(certDir, "dns/root-cert.pem")),
	})
	// A secret with -cacert in the suffix, referenced directly in the credential
	trickyCaCert = makeSecret("tricky-cacert", map[string]string{
		credentials.GenericScrtCert:   readFile(filepath.Join(certDir, "dns/cert-chain.pem")),
		credentials.GenericScrtKey:    readFile(filepath.Join(certDir, "dns/key.pem")),
		credentials.GenericScrtCaCert: readFile(filepath.Join(certDir, "dns/root-cert.pem")),
	})
	genericCert = makeSecret("generic", map[string]string{
		credentials.GenericScrtCert: readFile(filepath.Join(certDir, "default/cert-chain.pem")),
		credentials.GenericScrtKey:  readFile(filepath.Join(certDir, "default/key.pem")),
	})
	genericMtlsCert = makeSecret("generic-mtls", map[string]string{
		credentials.GenericScrtCert:   readFile(filepath.Join(certDir, "dns/cert-chain.pem")),
		credentials.GenericScrtKey:    readFile(filepath.Join(certDir, "dns/key.pem")),
		credentials.GenericScrtCaCert: readFile(filepath.Join(certDir, "dns/root-cert.pem")),
	})
	genericMtlsCertCrl = makeSecret("generic-mtls-crl", map[string]string{
		credentials.GenericScrtCert:   readFile(filepath.Join(certDir, "dns/cert-chain.pem")),
		credentials.GenericScrtKey:    readFile(filepath.Join(certDir, "dns/key.pem")),
		credentials.GenericScrtCaCert: readFile(filepath.Join(certDir, "dns/root-cert.pem")),
		credentials.GenericScrtCRL:    readFile(filepath.Join(certDir, "dns/cert-chain.pem")),
	})
	genericMtlsCertSplit = makeSecret("generic-mtls-split", map[string]string{
		credentials.GenericScrtCert: readFile(filepath.Join(certDir, "mountedcerts-client/cert-chain.pem")),
		credentials.GenericScrtKey:  readFile(filepath.Join(certDir, "mountedcerts-client/key.pem")),
	})
	genericMtlsCertSplitCa = makeSecret("generic-mtls-split-cacert", map[string]string{
		credentials.GenericScrtCaCert: readFile(filepath.Join(certDir, "mountedcerts-client/root-cert.pem")),
	})
	// Test secret for bug #58146 - same secret referenced with and without namespace
	bookinfoCert = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bookinfo-certs",
			Namespace: "bookinfo",
		},
		Data: map[string][]byte{
			credentials.GenericScrtCert: []byte(readFile(filepath.Join(certDir, "default/cert-chain.pem"))),
			credentials.GenericScrtKey:  []byte(readFile(filepath.Join(certDir, "default/key.pem"))),
		},
	}
)

func readFile(name string) string {
	cacert, _ := os.ReadFile(name)
	return string(cacert)
}

func TestGenerateSDS(t *testing.T) {
	type Expected struct {
		Key    string
		Cert   string
		CaCert string
		CaCrl  string
	}
	allResources := []string{
		"kubernetes://generic", "kubernetes://generic-mtls", "kubernetes://generic-mtls-cacert", "kubernetes://ca-only-cacert",
		"kubernetes://generic-mtls-split", "kubernetes://generic-mtls-split-cacert", "kubernetes://generic-mtls-crl",
		"kubernetes://generic-mtls-crl-cacert",
	}
	cases := []struct {
		name                 string
		proxy                *model.Proxy
		resources            []string
		request              *model.PushRequest
		expect               map[string]Expected
		accessReviewResponse func(action k8stesting.Action) (bool, runtime.Object, error)
		objects              []runtime.Object
	}{
		{
			name:      "simple",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true, Forced: true},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  string(genericCert.Data[credentials.GenericScrtKey]),
					Cert: string(genericCert.Data[credentials.GenericScrtCert]),
				},
			},
		},
		{
			name:      "sidecar",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true, Forced: true},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  string(genericCert.Data[credentials.GenericScrtKey]),
					Cert: string(genericCert.Data[credentials.GenericScrtCert]),
				},
			},
		},
		{
			name:      "unauthenticated",
			proxy:     &model.Proxy{Type: model.Router},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true, Forced: true},
			expect:    map[string]Expected{},
		},
		{
			name:      "multiple",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request:   &model.PushRequest{Full: true, Forced: true},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  string(genericCert.Data[credentials.GenericScrtKey]),
					Cert: string(genericCert.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://generic-mtls": {
					Key:  string(genericMtlsCert.Data[credentials.GenericScrtKey]),
					Cert: string(genericMtlsCert.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://ca-only-cacert": {
					CaCert: string(simpleCaCert.Data[credentials.GenericScrtCaCert]),
				},
				"kubernetes://generic-mtls-cacert": {
					CaCert: string(genericMtlsCert.Data[credentials.GenericScrtCaCert]),
				},
				"kubernetes://generic-mtls-split": {
					Key:  string(genericMtlsCertSplit.Data[credentials.GenericScrtKey]),
					Cert: string(genericMtlsCertSplit.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://generic-mtls-split-cacert": {
					CaCert: string(genericMtlsCertSplitCa.Data[credentials.GenericScrtCaCert]),
				},
				"kubernetes://generic-mtls-crl": {
					Key:  string(genericMtlsCertCrl.Data[credentials.GenericScrtKey]),
					Cert: string(genericMtlsCertCrl.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://generic-mtls-crl-cacert": {
					CaCert: string(genericMtlsCertCrl.Data[credentials.GenericScrtCaCert]),
					CaCrl:  string(genericMtlsCertCrl.Data[credentials.GenericScrtCRL]),
				},
			},
		},
		{
			name:      "full push with updates",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: []string{"kubernetes://generic", "kubernetes://generic-mtls", "kubernetes://generic-mtls-cacert"},
			request: &model.PushRequest{Full: true, ConfigsUpdated: sets.New(model.ConfigKey{
				Kind:      kind.Secret,
				Name:      "generic-mtls",
				Namespace: "istio-system",
			})},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  string(genericCert.Data[credentials.GenericScrtKey]),
					Cert: string(genericCert.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://generic-mtls": {
					Key:  string(genericMtlsCert.Data[credentials.GenericScrtKey]),
					Cert: string(genericMtlsCert.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://generic-mtls-cacert": {
					CaCert: string(genericMtlsCert.Data[credentials.GenericScrtCaCert]),
				},
			},
		},
		{
			name:      "incremental push with updates",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request:   &model.PushRequest{Full: false, ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "generic", Namespace: "istio-system"})},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  string(genericCert.Data[credentials.GenericScrtKey]),
					Cert: string(genericCert.Data[credentials.GenericScrtCert]),
				},
			},
		},
		{
			name:      "incremental push with updates - mtls",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request: &model.PushRequest{
				Full:           false,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "generic-mtls", Namespace: "istio-system"}),
			},
			expect: map[string]Expected{
				"kubernetes://generic-mtls": {
					Key:  string(genericMtlsCert.Data[credentials.GenericScrtKey]),
					Cert: string(genericMtlsCert.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://generic-mtls-cacert": {
					CaCert: string(genericMtlsCert.Data[credentials.GenericScrtCaCert]),
				},
			},
		},
		{
			name:      "incremental push with updates - mtls with crl",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request: &model.PushRequest{
				Full:           false,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "generic-mtls-crl", Namespace: "istio-system"}),
			},
			expect: map[string]Expected{
				"kubernetes://generic-mtls-crl": {
					Key:  string(genericMtlsCertCrl.Data[credentials.GenericScrtKey]),
					Cert: string(genericMtlsCertCrl.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://generic-mtls-crl-cacert": {
					CaCert: string(genericMtlsCertCrl.Data[credentials.GenericScrtCaCert]),
					CaCrl:  string(genericMtlsCertCrl.Data[credentials.GenericScrtCRL]),
				},
			},
		},
		{
			name:      "incremental push with updates - mtls split",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request: &model.PushRequest{
				Full:           false,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "generic-mtls-split", Namespace: "istio-system"}),
			},
			expect: map[string]Expected{
				"kubernetes://generic-mtls-split": {
					Key:  string(genericMtlsCertSplit.Data[credentials.GenericScrtKey]),
					Cert: string(genericMtlsCertSplit.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://generic-mtls-split-cacert": {
					CaCert: string(genericMtlsCertSplitCa.Data[credentials.GenericScrtCaCert]),
				},
			},
		},
		{
			name:      "incremental push with updates - mtls split ca update",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request: &model.PushRequest{
				Full:           false,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "generic-mtls-split-cacert", Namespace: "istio-system"}),
			},
			expect: map[string]Expected{
				"kubernetes://generic-mtls-split": {
					Key:  string(genericMtlsCertSplit.Data[credentials.GenericScrtKey]),
					Cert: string(genericMtlsCertSplit.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://generic-mtls-split-cacert": {
					CaCert: string(genericMtlsCertSplitCa.Data[credentials.GenericScrtCaCert]),
				},
			},
		},
		{
			// If an unknown resource is request, we return all the ones we do know about
			name:      "unknown",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: []string{"kubernetes://generic", "foo://invalid", "kubernetes://not-found", "default", "builtin://"},
			request:   &model.PushRequest{Full: true, Forced: true},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  string(genericCert.Data[credentials.GenericScrtKey]),
					Cert: string(genericCert.Data[credentials.GenericScrtCert]),
				},
			},
		},
		{
			// proxy without authorization
			name:      "unauthorized",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true, Forced: true},
			// Should get a response, but it will be empty
			expect: map[string]Expected{},
			accessReviewResponse: func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("not authorized")
			},
		},
		{
			// proxy without authorization -- can get CA certs only
			name:      "partially unauthorized",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: allResources,
			request:   &model.PushRequest{Full: true, Forced: true},
			// Should get a response, but it will be empty
			expect: map[string]Expected{
				"kubernetes://ca-only-cacert": {
					CaCert: string(simpleCaCert.Data[credentials.GenericScrtCaCert]),
				},
				// Note: below are a little strange, since we split a mTLS secret (with 3 parts) into 2 resources
				// We allow only the public part.
				// Ultimately it doesn't matter that we allow this; the client will need both parts to do anything
				"kubernetes://generic-mtls-split-cacert": {
					CaCert: string(genericMtlsCertSplitCa.Data[credentials.GenericScrtCaCert]),
				},
				"kubernetes://generic-mtls-crl-cacert": {
					CaCert: string(genericMtlsCertCrl.Data[credentials.GenericScrtCaCert]),
					CaCrl:  string(genericMtlsCertCrl.Data[credentials.GenericScrtCRL]),
				},
				"kubernetes://generic-mtls-cacert": {
					CaCert: string(genericMtlsCert.Data[credentials.GenericScrtCaCert]),
				},
			},
			accessReviewResponse: func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("not authorized")
			},
		},
		{
			// proxy without authorization -- can get CA certs only.
			// If a credential name is poorly named with a -cacert suffix, we shouldn't let that be accesses
			name:      "tricky cacert name",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"}, Type: model.Router},
			resources: []string{"kubernetes://tricky-cacert", "kubernetes://tricky-cacert-cacert"},
			request:   &model.PushRequest{Full: true, Forced: true},
			expect: map[string]Expected{
				// They should NOT be able to get the private key material
				"kubernetes://tricky-cacert": {
					CaCert: string(trickyCaCert.Data[credentials.GenericScrtCaCert]),
				},
				"kubernetes://tricky-cacert-cacert": {
					CaCert: string(trickyCaCert.Data[credentials.GenericScrtCaCert]),
				},
			},
			objects: []runtime.Object{trickyCaCert},
			accessReviewResponse: func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("not authorized")
			},
		},
		{
			// Regression test for https://github.com/istio/istio/issues/58146
			// Verifies that the same k8s Secret referenced as both `secret-name` and `namespace/secret-name`
			// returns secrets with different resource names, preventing cache key collisions.
			name:      "same secret with and without namespace in resource name",
			proxy:     &model.Proxy{VerifiedIdentity: &spiffe.Identity{Namespace: "bookinfo"}, Type: model.Router},
			resources: []string{"kubernetes://bookinfo-certs", "kubernetes://bookinfo/bookinfo-certs"},
			request:   &model.PushRequest{Full: true, Forced: true},
			expect: map[string]Expected{
				"kubernetes://bookinfo-certs": {
					Key:  string(bookinfoCert.Data[credentials.GenericScrtKey]),
					Cert: string(bookinfoCert.Data[credentials.GenericScrtCert]),
				},
				"kubernetes://bookinfo/bookinfo-certs": {
					Key:  string(bookinfoCert.Data[credentials.GenericScrtKey]),
					Cert: string(bookinfoCert.Data[credentials.GenericScrtCert]),
				},
			},
			objects: []runtime.Object{bookinfoCert},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.proxy.Metadata == nil {
				tt.proxy.Metadata = &model.NodeMetadata{}
			}
			obj := tt.objects
			if obj == nil {
				// Default set
				obj = []runtime.Object{
					genericCert,
					genericMtlsCert,
					simpleCaCert,
					genericMtlsCertCrl,
					genericMtlsCertSplit,
					genericMtlsCertSplitCa,
				}
			}
			tt.proxy.Metadata.ClusterID = constants.DefaultClusterName
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				KubernetesObjects: obj,
			})
			cc := s.KubeClient().Kube().(*fake.Clientset)

			cc.Fake.Lock()
			if tt.accessReviewResponse != nil {
				cc.Fake.PrependReactor("create", "subjectaccessreviews", tt.accessReviewResponse)
			} else {
				xds.DisableAuthorizationForSecret(cc)
			}
			cc.Fake.Unlock()

			gen := s.Discovery.Generators[v3.SecretType]
			tt.request.Start = time.Now()
			secrets, _, _ := gen.Generate(s.SetupProxy(tt.proxy), &model.WatchedResource{ResourceNames: sets.New(tt.resources...)}, tt.request)
			raw := xdstest.ExtractTLSSecrets(t, xdsserver.ResourcesToAny(secrets))

			got := map[string]Expected{}
			for _, scrt := range raw {
				got[scrt.Name] = Expected{
					Key:    string(scrt.GetTlsCertificate().GetPrivateKey().GetInlineBytes()),
					Cert:   string(scrt.GetTlsCertificate().GetCertificateChain().GetInlineBytes()),
					CaCert: string(scrt.GetValidationContext().GetTrustedCa().GetInlineBytes()),
					CaCrl:  string(scrt.GetValidationContext().GetCrl().GetInlineBytes()),
				}
			}
			assert.Equal(t, got, tt.expect)
		})
	}
}

// TestCaching ensures we don't have cross-proxy cache generation issues. This is split from TestGenerate
// since it is order dependent.
// Regression test for https://github.com/istio/istio/issues/33368
func TestCaching(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		KubernetesObjects: []runtime.Object{genericCert},
		KubeClientModifier: func(c kube.Client) {
			cc := c.Kube().(*fake.Clientset)
			xds.DisableAuthorizationForSecret(cc)
		},
	})
	gen := s.Discovery.Generators[v3.SecretType]

	fullPush := &model.PushRequest{Full: true, Start: time.Now(), Forced: true}
	istiosystem := &model.Proxy{
		Metadata:         &model.NodeMetadata{ClusterID: constants.DefaultClusterName},
		VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"},
		Type:             model.Router,
		ConfigNamespace:  "istio-system",
	}
	otherNamespace := &model.Proxy{
		Metadata:         &model.NodeMetadata{ClusterID: constants.DefaultClusterName},
		VerifiedIdentity: &spiffe.Identity{Namespace: "other-namespace"},
		Type:             model.Router,
		ConfigNamespace:  "other-namespace",
	}

	secrets, _, _ := gen.Generate(s.SetupProxy(istiosystem), &model.WatchedResource{ResourceNames: sets.New("kubernetes://generic")}, fullPush)
	raw := xdstest.ExtractTLSSecrets(t, xdsserver.ResourcesToAny(secrets))
	if len(raw) != 1 {
		t.Fatalf("failed to get expected secrets for authorized proxy: %v", raw)
	}

	// We should not get secret returned, even though we are asking for the same one
	secrets, _, _ = gen.Generate(s.SetupProxy(otherNamespace), &model.WatchedResource{ResourceNames: sets.New("kubernetes://generic")}, fullPush)
	raw = xdstest.ExtractTLSSecrets(t, xdsserver.ResourcesToAny(secrets))
	if len(raw) != 0 {
		t.Fatalf("failed to get expected secrets for unauthorized proxy: %v", raw)
	}
}

func TestPrivateKeyProviderProxyConfig(t *testing.T) {
	pkpProxy := &model.Proxy{
		VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"},
		Type:             model.Router,
		Metadata: &model.NodeMetadata{
			ClusterID: constants.DefaultClusterName,
			ProxyConfig: &model.NodeMetaProxyConfig{
				PrivateKeyProvider: &meshconfig.PrivateKeyProvider{
					Provider: &meshconfig.PrivateKeyProvider_Cryptomb{
						Cryptomb: &meshconfig.PrivateKeyProvider_CryptoMb{
							PollDelay: &durationpb.Duration{
								Seconds: 0,
								Nanos:   10000,
							},
						},
					},
				},
			},
		},
	}
	rawProxy := &model.Proxy{
		VerifiedIdentity: &spiffe.Identity{Namespace: "istio-system"},
		Type:             model.Router,
		Metadata:         &model.NodeMetadata{ClusterID: constants.DefaultClusterName},
	}
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		KubernetesObjects: []runtime.Object{genericCert},
		KubeClientModifier: func(c kube.Client) {
			cc := c.Kube().(*fake.Clientset)
			xds.DisableAuthorizationForSecret(cc)
		},
	})
	gen := s.Discovery.Generators[v3.SecretType]
	fullPush := &model.PushRequest{Full: true, Start: time.Now(), Forced: true}
	secrets, _, _ := gen.Generate(s.SetupProxy(rawProxy), &model.WatchedResource{ResourceNames: sets.New("kubernetes://generic")}, fullPush)
	raw := xdstest.ExtractTLSSecrets(t, xdsserver.ResourcesToAny(secrets))
	for _, scrt := range raw {
		if scrt.GetTlsCertificate().GetPrivateKeyProvider() != nil {
			t.Fatal("expect no private key provider in secret")
		}
	}

	// add private key provider in proxy-config
	secrets, _, _ = gen.Generate(s.SetupProxy(pkpProxy), &model.WatchedResource{ResourceNames: sets.New("kubernetes://generic")}, fullPush)
	raw = xdstest.ExtractTLSSecrets(t, xdsserver.ResourcesToAny(secrets))
	for _, scrt := range raw {
		privateKeyProvider := scrt.GetTlsCertificate().GetPrivateKeyProvider()
		if privateKeyProvider == nil {
			t.Fatal("expect private key provider in secret")
		}
		if privateKeyProvider.GetFallback() {
			t.Fatal("expect fallback for private key provider in secret as false")
		}
	}

	// erase private key provider in proxy-config
	secrets, _, _ = gen.Generate(s.SetupProxy(rawProxy), &model.WatchedResource{ResourceNames: sets.New("kubernetes://generic")}, fullPush)
	raw = xdstest.ExtractTLSSecrets(t, xdsserver.ResourcesToAny(secrets))
	for _, scrt := range raw {
		if scrt.GetTlsCertificate().GetPrivateKeyProvider() != nil {
			t.Fatal("expect no private key provider in secret")
		}
	}
}
