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

// Package clustertrustbundletest provides fake discovery and certificate helpers.
// Set discovery resources before starting watchers: FakeDiscovery reads Resources without a lock.
// Runtime changes use reactors and atomics instead.
package clustertrustbundletest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"go.uber.org/atomic"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

const resource = "clustertrustbundles"

var groupVersion = certificatesv1.SchemeGroupVersion.String()

func fakeDiscovery(client kube.Client) *fakediscovery.FakeDiscovery {
	return client.Kube().Discovery().(*fakediscovery.FakeDiscovery)
}

func apiResourceList() *metav1.APIResourceList {
	return &metav1.APIResourceList{
		GroupVersion: groupVersion,
		APIResources: []metav1.APIResource{{
			Name:       resource,
			Kind:       "ClusterTrustBundle",
			Namespaced: false,
			Verbs:      metav1.Verbs{"get", "list", "watch"},
		}},
	}
}

// SetAPIAvailable must be called before discovery starts. Use NewAPIToggle for an API that appears later.
func SetAPIAvailable(client kube.Client) {
	fd := fakeDiscovery(client)
	fd.Resources = append(fd.Resources, apiResourceList())
}

// APIToggle controls when a fake client starts serving the ClusterTrustBundle API.
type APIToggle struct {
	enabled atomic.Bool
}

// NewAPIToggle makes discovery answer NotFound for the ClusterTrustBundle group version until Enable is called. Like
// SetAPIAvailable it must be called before anything that probes discovery is created.
func NewAPIToggle(client kube.Client) *APIToggle {
	t := &APIToggle{}
	fd := fakeDiscovery(client)
	fd.Resources = append(fd.Resources, apiResourceList())
	fd.PrependReactor("get", "resource", func(clienttesting.Action) (bool, runtime.Object, error) {
		if t.enabled.Load() {
			return false, nil, nil
		}
		return true, nil, errors.NewNotFound(certificatesv1.Resource(resource), groupVersion)
	})
	return t
}

// Enable makes the API served from now on. It is safe to call at any time.
func (t *APIToggle) Enable() {
	t.enabled.Store(true)
}

// InjectDiscoveryErrors counts discovery calls and injects errors. fail must be safe for concurrent use.
func InjectDiscoveryErrors(client kube.Client, fail func() error) *atomic.Int32 {
	calls := atomic.NewInt32(0)
	fd := fakeDiscovery(client)
	fd.PrependReactor("get", "resource", func(clienttesting.Action) (bool, runtime.Object, error) {
		calls.Inc()
		if err := fail(); err != nil {
			return true, nil, err
		}
		return false, nil, nil
	})
	return calls
}

// GenerateCA returns a PEM-encoded self-signed CA certificate, valid for one hour.
func GenerateCA(t testing.TB, cn string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	assert.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
