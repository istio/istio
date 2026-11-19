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

// Package clustertrustbundle provides building blocks for reading certificates.k8s.io/v1 ClusterTrustBundles.
//
// Istio's generated schema registers ClusterTrustBundle as v1beta1, which is not served by default. This package
// registers the v1 type by hand and offers a discovery-based watcher so consumers can degrade gracefully on clusters
// that do not serve the API.
package clustertrustbundle

import (
	"context"
	"crypto/x509"
	"fmt"
	"strings"

	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	"istio.io/istio/pkg/config/schema/kubeclient"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
)

var (
	GVR = certificatesv1.SchemeGroupVersion.WithResource("clustertrustbundles")
	GVK = certificatesv1.SchemeGroupVersion.WithKind("ClusterTrustBundle")
)

func init() {
	kubeclient.Register[*certificatesv1.ClusterTrustBundle](
		GVR,
		GVK,
		func(ctx context.Context, c kubeclient.ClientGetter, _ string, o metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().CertificatesV1().ClusterTrustBundles().List(ctx, o)
		},
		func(ctx context.Context, c kubeclient.ClientGetter, _ string, o metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().CertificatesV1().ClusterTrustBundles().Watch(ctx, o)
		},
		func(c kubeclient.ClientGetter, _ string) kubetypes.WriteAPI[*certificatesv1.ClusterTrustBundle] {
			return c.Kube().CertificatesV1().ClusterTrustBundles()
		},
	)
}

// NewInformer returns a read-only informer for v1 ClusterTrustBundles that stays empty until the watcher reports the API as served.
// No object filter is applied: ClusterTrustBundle is cluster-scoped, so namespace discovery selectors do not apply.
func NewInformer(client kube.Client, watcher *APIWatcher) kclient.Informer[*certificatesv1.ClusterTrustBundle] {
	return kclient.NewDelayedInformerWithFilter[*certificatesv1.ClusterTrustBundle](
		client, GVR, kubetypes.StandardInformer, kclient.Filter{}, watcher)
}

// ParseTrustBundle validates the trust bundle of a ClusterTrustBundle and returns it as PEM bytes.
// Error messages are suitable for surfacing in a user-facing status condition.
func ParseTrustBundle(b *certificatesv1.ClusterTrustBundle) ([]byte, error) {
	if b == nil {
		return nil, fmt.Errorf("ClusterTrustBundle is nil")
	}
	if strings.TrimSpace(b.Spec.TrustBundle) == "" {
		return nil, fmt.Errorf("ClusterTrustBundle %q has an empty spec.trustBundle", b.Name)
	}
	pem := []byte(b.Spec.TrustBundle)
	if !x509.NewCertPool().AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("ClusterTrustBundle %q does not contain any valid PEM-encoded CA certificates in spec.trustBundle", b.Name)
	}
	return pem, nil
}
