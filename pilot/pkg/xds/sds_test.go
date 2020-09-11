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

package xds

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/pilot/pkg/model"
	kubesecrets "istio.io/istio/pilot/pkg/secrets/kube"
	authnmodel "istio.io/istio/pilot/pkg/security/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/schema/gvk"
)

func TestParseResourceName(t *testing.T) {
	cases := []struct {
		name             string
		resource         string
		defaultNamespace string
		expected         SecretResource
		err              bool
	}{
		{
			name:             "simple",
			resource:         "kubernetes://cert",
			defaultNamespace: "default",
			expected: SecretResource{
				Type:         authnmodel.KubernetesSecretType,
				Name:         "cert",
				Namespace:    "default",
				ResourceName: "kubernetes://cert",
			},
		},
		{
			name:             "with namespace",
			resource:         "kubernetes://namespace/cert",
			defaultNamespace: "default",
			expected: SecretResource{
				Type:         authnmodel.KubernetesSecretType,
				Name:         "cert",
				Namespace:    "namespace",
				ResourceName: "kubernetes://namespace/cert",
			},
		},
		{
			name:             "plain",
			resource:         "cert",
			defaultNamespace: "default",
			err:              true,
		},
		{
			name:             "non kubernetes",
			resource:         "vault://cert",
			defaultNamespace: "default",
			err:              true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseResourceName(tt.resource, tt.defaultNamespace)
			if tt.err != (err != nil) {
				t.Fatalf("expected err=%v but got err=%v", tt.err, err)
			}
			if got != tt.expected {
				t.Fatalf("want %+v, got %+v", tt.expected, got)
			}
		})
	}
}

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
	genericCert = makeSecret("generic", map[string]string{
		kubesecrets.GenericScrtCert: "generic-cert", kubesecrets.GenericScrtKey: "generic-key",
	})
	genericMtlsCert = makeSecret("generic-mtls", map[string]string{
		kubesecrets.GenericScrtCert: "generic-mtls-cert", kubesecrets.GenericScrtKey: "generic-mtls-key", kubesecrets.GenericScrtCaCert: "generic-mtls-ca",
	})
	genericMtlsCertSplit = makeSecret("generic-mtls-split", map[string]string{
		kubesecrets.GenericScrtCert: "generic-mtls-split-cert", kubesecrets.GenericScrtKey: "generic-mtls-split-key",
	})
	genericMtlsCertSplitCa = makeSecret("generic-mtls-split-cacert", map[string]string{
		kubesecrets.GenericScrtCaCert: "generic-mtls-split-ca",
	})
)

func TestGenerate(t *testing.T) {
	s := NewFakeDiscoveryServer(t, FakeOptions{
		KubernetesObjects: []runtime.Object{genericCert, genericMtlsCert, genericMtlsCertSplit, genericMtlsCertSplitCa},
	})
	gen := s.Discovery.Generators[v3.SecretType]
	type Expected struct {
		Key    string
		Cert   string
		CaCert string
	}
	cases := []struct {
		name      string
		proxy     *model.Proxy
		resources []string
		request   *model.PushRequest
		expect    map[string]Expected
	}{
		{
			name:      "simple",
			proxy:     &model.Proxy{Type: model.Router, ConfigNamespace: "istio-system"},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  "generic-key",
					Cert: "generic-cert",
				},
			},
		},
		{
			name:      "sidecar",
			proxy:     &model.Proxy{ConfigNamespace: "istio-system"},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true},
			expect:    map[string]Expected{},
		},
		{
			name:      "mismatched namespace",
			proxy:     &model.Proxy{Type: model.Router},
			resources: []string{"kubernetes://generic"},
			request:   &model.PushRequest{Full: true},
			expect:    map[string]Expected{},
		},
		{
			name:  "multiple",
			proxy: &model.Proxy{Type: model.Router, ConfigNamespace: "istio-system"},
			resources: []string{"kubernetes://generic", "kubernetes://generic-mtls", "kubernetes://generic-mtls-cacert",
				"kubernetes://generic-mtls-split", "kubernetes://generic-mtls-split-cacert"},
			request: &model.PushRequest{Full: true},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  "generic-key",
					Cert: "generic-cert",
				},
				"kubernetes://generic-mtls": {
					Key:  "generic-mtls-key",
					Cert: "generic-mtls-cert",
				},
				"kubernetes://generic-mtls-cacert": {
					CaCert: "generic-mtls-ca",
				},
				"kubernetes://generic-mtls-split": {
					Key:  "generic-mtls-split-key",
					Cert: "generic-mtls-split-cert",
				},
				"kubernetes://generic-mtls-split-cacert": {
					CaCert: "generic-mtls-split-ca",
				},
			},
		},
		{
			name:      "full push with updates",
			proxy:     &model.Proxy{Type: model.Router, ConfigNamespace: "istio-system"},
			resources: []string{"kubernetes://generic", "kubernetes://generic-mtls"},
			request: &model.PushRequest{Full: true, ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "generic-mtls", Namespace: "istio-system", Kind: gvk.Secret}: {},
			}},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  "generic-key",
					Cert: "generic-cert",
				},
				"kubernetes://generic-mtls": {
					Key:  "generic-mtls-key",
					Cert: "generic-mtls-cert",
				},
			},
		},
		{
			name:      "incremental push with updates",
			proxy:     &model.Proxy{Type: model.Router, ConfigNamespace: "istio-system"},
			resources: []string{"kubernetes://generic", "kubernetes://generic-mtls"},
			request: &model.PushRequest{Full: false, ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Name: "generic-mtls", Namespace: "istio-system", Kind: gvk.Secret}: {},
			}},
			expect: map[string]Expected{
				"kubernetes://generic-mtls": {
					Key:  "generic-mtls-key",
					Cert: "generic-mtls-cert",
				},
			},
		},
		{
			// If an unknown resource is request, we return all the ones we do know about
			name:      "unknown",
			proxy:     &model.Proxy{Type: model.Router, ConfigNamespace: "istio-system"},
			resources: []string{"kubernetes://generic", "foo://invalid", "kubernetes://not-found"},
			request:   &model.PushRequest{Full: true},
			expect: map[string]Expected{
				"kubernetes://generic": {
					Key:  "generic-key",
					Cert: "generic-cert",
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			raw := xdstest.ExtractTLSSecrets(t, gen.Generate(s.SetupProxy(tt.proxy), s.PushContext(),
				&model.WatchedResource{ResourceNames: tt.resources}, tt.request))
			got := map[string]Expected{}
			for _, scrt := range raw {
				got[scrt.Name] = Expected{
					Key:    string(scrt.GetTlsCertificate().GetPrivateKey().GetInlineBytes()),
					Cert:   string(scrt.GetTlsCertificate().GetCertificateChain().GetInlineBytes()),
					CaCert: string(scrt.GetValidationContext().GetTrustedCa().GetInlineBytes()),
				}
			}
			if diff := cmp.Diff(got, tt.expect); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
