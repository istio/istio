// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package clustertrustbundle

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
)

const defaultTrustBundleName = "istio.io:istiod-ca:root-cert"

func TestController(t *testing.T) {
	initialRootCert := "root"
	testCases := []struct {
		name              string
		trustBundleName   string
		rootcertUpdate    string
		trustbundleUpdate string
	}{
		{
			name:           "rootcert_update",
			rootcertUpdate: "abc",
		},
		{
			name:              "trustbundle_update",
			trustbundleUpdate: "def",
		},
		{
			name:            "custom_trustbundle_name",
			trustBundleName: "istio.io:istiod-ca:custom-root-cert",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			expectedTrustBundleName := defaultTrustBundleName
			if tc.trustBundleName != "" {
				expectedTrustBundleName = tc.trustBundleName
				old := features.ClusterTrustBundleName
				features.ClusterTrustBundleName = tc.trustBundleName
				t.Cleanup(func() {
					features.ClusterTrustBundleName = old
				})
			}

			clientSet := kube.NewFakeClient()
			stop := test.NewStop(t)
			watcher := keycertbundle.NewWatcher()
			watcher.SetAndNotify(nil, nil, []byte(initialRootCert))
			controller := NewController(clientSet, watcher)
			clientSet.RunAndWait(stop)
			go controller.Run(stop)

			g.Expect(controller.trustBundleName).To(Equal(expectedTrustBundleName))

			g.Eventually(func() {
				trustbundle := controller.clustertrustbundles.Get(expectedTrustBundleName, "")
				g.Expect(trustbundle).ToNot(BeNil())
				g.Expect(trustbundle.Spec.TrustBundle).To(Equal(initialRootCert))
				g.Expect(trustbundle.Spec.SignerName).To(Equal(istioClusterTrustBundleSignerName))
			})

			if tc.trustBundleName != "" {
				// The controller must not also write the default-named bundle. Query the API
				// directly, since the filtered informer would hide it.
				_, err := clientSet.Kube().CertificatesV1beta1().ClusterTrustBundles().
					Get(context.Background(), defaultTrustBundleName, v1.GetOptions{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "unexpected object %q: %v", defaultTrustBundleName, err)
			}

			expectedTrustBundle := initialRootCert
			if tc.rootcertUpdate != "" {
				watcher.SetAndNotify(nil, nil, []byte(tc.rootcertUpdate))
				expectedTrustBundle = tc.rootcertUpdate
			} else if tc.trustbundleUpdate != "" {
				controller.clustertrustbundles.Update(&certificatesv1beta1.ClusterTrustBundle{
					ObjectMeta: v1.ObjectMeta{
						Name: expectedTrustBundleName,
					},
					Spec: certificatesv1beta1.ClusterTrustBundleSpec{
						SignerName:  istioClusterTrustBundleSignerName,
						TrustBundle: tc.trustbundleUpdate,
					},
				})
			}

			g.Eventually(func() {
				trustbundle := controller.clustertrustbundles.Get(expectedTrustBundleName, "")
				g.Expect(trustbundle).ToNot(BeNil())
				g.Expect(trustbundle.Spec.TrustBundle).To(Equal(expectedTrustBundle))
			})
		})
	}
}
