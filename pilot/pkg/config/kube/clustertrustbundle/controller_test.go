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
	"testing"

	. "github.com/onsi/gomega"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
)

func TestController(t *testing.T) {
	initialRootCert := "root"
	g := NewWithT(t)
	testCases := []struct {
		name              string
		rootcertUpdate    string
		trustbundleUpdate string
	}{
		{
			name:           "rootcert_update",
			rootcertUpdate: "abc",
		},
		{
			name:              "rootcert_update",
			trustbundleUpdate: "def",
		},
	}

	clientSet := kube.NewFakeClient()
	stop := test.NewStop(t)
	watcher := keycertbundle.NewWatcher()
	watcher.SetAndNotify(nil, nil, []byte(initialRootCert))
	controller := NewController(clientSet, watcher)
	clientSet.RunAndWait(stop)
	go controller.Run(stop)

	g.Eventually(func() {
		trustbundle := controller.clustertrustbundles.Get(istioClusterTrustBundleName, "")
		g.Expect(trustbundle).ToNot(BeNil())
		g.Expect(trustbundle.Spec.TrustBundle).To(Equal(initialRootCert))
	})

	for _, tc := range testCases {
		expectedTrustBundle := initialRootCert
		if tc.rootcertUpdate != "" {
			watcher.SetAndNotify(nil, nil, []byte(tc.rootcertUpdate))
			expectedTrustBundle = tc.rootcertUpdate
		} else if tc.trustbundleUpdate != "" {
			controller.clustertrustbundles.Update(&certificatesv1beta1.ClusterTrustBundle{
				ObjectMeta: v1.ObjectMeta{
					Name: istioClusterTrustBundleName,
				},
				Spec: certificatesv1beta1.ClusterTrustBundleSpec{
					SignerName:  istioClusterTrustBundleSignerName,
					TrustBundle: tc.trustbundleUpdate,
				},
			})
		}

		g.Eventually(func() {
			trustbundle := controller.clustertrustbundles.Get(istioClusterTrustBundleName, "")
			g.Expect(trustbundle).ToNot(BeNil())
			g.Expect(trustbundle.Spec.TrustBundle).To(Equal(expectedTrustBundle))
		})
	}
}
