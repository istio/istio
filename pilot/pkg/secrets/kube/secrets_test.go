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

package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/pkg/kube"
)

func makeSecret(name string, data map[string]string) *corev1.Secret {
	bdata := map[string][]byte{}
	for k, v := range data {
		bdata[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Data: bdata,
	}
}

var (
	genericCert = makeSecret("generic", map[string]string{
		GenericScrtCert: "generic-cert", GenericScrtKey: "generic-key",
	})
	genericMtlsCert = makeSecret("generic-mtls", map[string]string{
		GenericScrtCert: "generic-mtls-cert", GenericScrtKey: "generic-mtls-key", GenericScrtCaCert: "generic-mtls-ca",
	})
	genericMtlsCertSplit = makeSecret("generic-mtls-split", map[string]string{
		GenericScrtCert: "generic-mtls-split-cert", GenericScrtKey: "generic-mtls-split-key",
	})
	genericMtlsCertSplitCa = makeSecret("generic-mtls-split-cacert", map[string]string{
		GenericScrtCaCert: "generic-mtls-split-ca",
	})
	tlsCert = makeSecret("tls", map[string]string{
		TLSSecretCert: "tls-cert", TLSSecretKey: "tls-key",
	})
	tlsMtlsCert = makeSecret("tls-mtls", map[string]string{
		TLSSecretCert: "tls-mtls-cert", TLSSecretKey: "tls-mtls-key", TLSSecretCaCert: "tls-mtls-ca",
	})
	tlsMtlsCertSplit = makeSecret("tls-mtls-split", map[string]string{
		TLSSecretCert: "tls-mtls-split-cert", TLSSecretKey: "tls-mtls-split-key",
	})
	tlsMtlsCertSplitCa = makeSecret("tls-mtls-split-cacert", map[string]string{
		TLSSecretCaCert: "tls-mtls-split-ca",
	})
)

func TestSecretsController(t *testing.T) {
	secrets := []runtime.Object{
		genericCert,
		genericMtlsCert,
		genericMtlsCertSplit,
		genericMtlsCertSplitCa,
		tlsCert,
		tlsMtlsCert,
		tlsMtlsCertSplit,
		tlsMtlsCertSplitCa,
	}
	client := kube.NewFakeClient(secrets...)
	sc := NewSecretsController(client, "", nil)
	client.RunAndWait(make(chan struct{}))
	cases := []struct {
		name      string
		namespace string
		cert      string
		key       string
		caCert    string
	}{
		{"generic", "default", "generic-cert", "generic-key", ""},
		{"generic-mtls", "default", "generic-mtls-cert", "generic-mtls-key", "generic-mtls-ca"},
		{"generic-mtls-split", "default", "generic-mtls-split-cert", "generic-mtls-split-key", ""},
		{"generic-mtls-split-cacert", "default", "", "", "generic-mtls-split-ca"},
		{"tls", "default", "tls-cert", "tls-key", ""},
		{"tls-mtls", "default", "tls-mtls-cert", "tls-mtls-key", "tls-mtls-ca"},
		{"tls-mtls-split", "default", "tls-mtls-split-cert", "tls-mtls-split-key", ""},
		{"tls-mtls-split-cacert", "default", "", "", "tls-mtls-split-ca"},
		{"generic", "wrong-namespace", "", "", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			key, cert := sc.GetKeyAndCert(tt.name, tt.namespace)
			if tt.key != string(key) {
				t.Errorf("got key %q, wanted %q", string(key), tt.key)
			}
			if tt.cert != string(cert) {
				t.Errorf("got cert %q, wanted %q", string(cert), tt.cert)
			}
			caCert := sc.GetCaCert(tt.name, tt.namespace)
			if tt.caCert != string(caCert) {
				t.Errorf("got caCert %q, wanted %q", string(caCert), tt.caCert)
			}
		})
	}
}
