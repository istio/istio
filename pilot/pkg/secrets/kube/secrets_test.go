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
	"fmt"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/pilot/pkg/util/sets"
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
	overlapping = makeSecret("overlap", map[string]string{
		GenericScrtCert: "cert", GenericScrtKey: "key", GenericScrtCaCert: "main-ca",
	})
	overlappingCa = makeSecret("overlap-cacert", map[string]string{
		GenericScrtCaCert: "split-ca",
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
		overlapping,
		overlappingCa,
		tlsCert,
		tlsMtlsCert,
		tlsMtlsCertSplit,
		tlsMtlsCertSplitCa,
	}
	client := kube.NewFakeClient(secrets...)
	sc := NewSecretsController(client, "")
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
		// The -cacert secret has precedence
		{"overlap-cacert", "default", "", "", "split-ca"},
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

func allowIdentities(c kube.Client, identities ...string) {
	allowed := sets.NewSet(identities...)
	c.Kube().(*fake.Clientset).Fake.PrependReactor("create", "subjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		a := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		if allowed.Contains(a.Spec.User) {
			return true, &authorizationv1.SubjectAccessReview{
				Status: authorizationv1.SubjectAccessReviewStatus{
					Allowed: true,
				},
			}, nil
		}
		return true, &authorizationv1.SubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: false,
				Reason:  fmt.Sprintf("user %s cannot access secrets", a.Spec.User),
			},
		}, nil
	})
}

func TestForCluster(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	localClient := kube.NewFakeClient()
	remoteClient := kube.NewFakeClient()
	sc := NewMulticluster(localClient, "local", "", stop)
	sc.addMemberCluster(remoteClient, "remote")
	sc.addMemberCluster(remoteClient, "remote2")
	cases := []struct {
		cluster string
		allowed bool
	}{
		{"local", true},
		{"remote", true},
		{"remote2", true},
		{"invalid", false},
	}
	for _, tt := range cases {
		t.Run(tt.cluster, func(t *testing.T) {
			_, err := sc.ForCluster(tt.cluster)
			if (err == nil) != tt.allowed {
				t.Fatalf("expected allowed=%v, got err=%v", tt.allowed, err)
			}
		})
	}
}

func TestAuthorize(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	localClient := kube.NewFakeClient()
	remoteClient := kube.NewFakeClient()
	allowIdentities(localClient, "system:serviceaccount:ns-local:sa-allowed")
	allowIdentities(remoteClient, "system:serviceaccount:ns-remote:sa-allowed")
	sc := NewMulticluster(localClient, "local", "", stop)
	sc.addMemberCluster(remoteClient, "remote")
	cases := []struct {
		sa      string
		ns      string
		cluster string
		allowed bool
	}{
		{"sa-denied", "ns-local", "local", false},
		{"sa-allowed", "ns-local", "local", true},
		{"sa-denied", "ns-local", "remote", false},
		{"sa-allowed", "ns-local", "remote", false},
		{"sa-denied", "ns-remote", "local", false},
		{"sa-allowed", "ns-remote", "local", false},
		{"sa-denied", "ns-remote", "remote", false},
		{"sa-allowed", "ns-remote", "remote", true},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%v/%v/%v", tt.sa, tt.ns, tt.cluster), func(t *testing.T) {
			con, err := sc.ForCluster(tt.cluster)
			if err != nil {
				t.Fatal(err)
			}
			got := con.Authorize(tt.sa, tt.ns)
			if (got == nil) != tt.allowed {
				t.Fatalf("expected allowed=%v, got error=%v", tt.allowed, got)
			}
		})
	}
}

func TestSecretsControllerMulticluster(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	secretsLocal := []runtime.Object{
		tlsCert,
		tlsMtlsCert,
		tlsMtlsCertSplit,
		tlsMtlsCertSplitCa,
	}
	tlsCertModified := makeSecret("tls", map[string]string{
		TLSSecretCert: "tls-cert-mod", TLSSecretKey: "tls-key",
	})
	secretsRemote := []runtime.Object{
		tlsCertModified,
		genericCert,
		genericMtlsCert,
		genericMtlsCertSplit,
		genericMtlsCertSplitCa,
	}

	localClient := kube.NewFakeClient(secretsLocal...)
	remoteClient := kube.NewFakeClient(secretsRemote...)
	otherRemoteClient := kube.NewFakeClient()
	sc := NewMulticluster(localClient, "local", "", stop)
	sc.addMemberCluster(remoteClient, "remote")
	sc.addMemberCluster(otherRemoteClient, "other")
	cases := []struct {
		name      string
		namespace string
		cluster   string
		cert      string
		key       string
		caCert    string
	}{
		// From local cluster
		// These are only in remote cluster, we do not have access
		{"generic", "default", "local", "", "", ""},
		{"generic-mtls", "default", "local", "", "", ""},
		{"generic-mtls-split", "default", "local", "", "", ""},
		{"generic-mtls-split-cacert", "default", "local", "", "", ""},
		// These are in local cluster, we can access
		{"tls", "default", "local", "tls-cert", "tls-key", ""},
		{"tls-mtls", "default", "local", "tls-mtls-cert", "tls-mtls-key", "tls-mtls-ca"},
		{"tls-mtls-split", "default", "local", "tls-mtls-split-cert", "tls-mtls-split-key", ""},
		{"tls-mtls-split-cacert", "default", "local", "", "", "tls-mtls-split-ca"},
		{"generic", "wrong-namespace", "local", "", "", ""},

		// From remote cluster
		// We can access all credentials - local and remote
		{"generic", "default", "remote", "generic-cert", "generic-key", ""},
		{"generic-mtls", "default", "remote", "generic-mtls-cert", "generic-mtls-key", "generic-mtls-ca"},
		{"generic-mtls-split", "default", "remote", "generic-mtls-split-cert", "generic-mtls-split-key", ""},
		{"generic-mtls-split-cacert", "default", "remote", "", "", "generic-mtls-split-ca"},
		// This is present in local and remote, but with a different value. We have the remote.
		{"tls", "default", "remote", "tls-cert-mod", "tls-key", ""},
		{"tls-mtls", "default", "remote", "tls-mtls-cert", "tls-mtls-key", "tls-mtls-ca"},
		{"tls-mtls-split", "default", "remote", "tls-mtls-split-cert", "tls-mtls-split-key", ""},
		{"tls-mtls-split-cacert", "default", "remote", "", "", "tls-mtls-split-ca"},
		{"generic", "wrong-namespace", "remote", "", "", ""},

		// From other remote cluster
		// We have no in cluster credentials; can only access those in config cluster
		{"generic", "default", "other", "", "", ""},
		{"generic-mtls", "default", "other", "", "", ""},
		{"generic-mtls-split", "default", "other", "", "", ""},
		{"generic-mtls-split-cacert", "default", "other", "", "", ""},
		{"tls", "default", "other", "tls-cert", "tls-key", ""},
		{"tls-mtls", "default", "other", "tls-mtls-cert", "tls-mtls-key", "tls-mtls-ca"},
		{"tls-mtls-split", "default", "other", "tls-mtls-split-cert", "tls-mtls-split-key", ""},
		{"tls-mtls-split-cacert", "default", "other", "", "", "tls-mtls-split-ca"},
		{"generic", "wrong-namespace", "other", "", "", ""},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%s-%v", tt.name, tt.cluster), func(t *testing.T) {
			con, err := sc.ForCluster(tt.cluster)
			if err != nil {
				t.Fatal(err)
			}
			key, cert := con.GetKeyAndCert(tt.name, tt.namespace)
			if tt.key != string(key) {
				t.Errorf("got key %q, wanted %q", string(key), tt.key)
			}
			if tt.cert != string(cert) {
				t.Errorf("got cert %q, wanted %q", string(cert), tt.cert)
			}
			caCert := con.GetCaCert(tt.name, tt.namespace)
			if tt.caCert != string(caCert) {
				t.Errorf("got caCert %q, wanted %q", string(caCert), tt.caCert)
			}
		})
	}
}
