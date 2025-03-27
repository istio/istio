package clustertrustbundle

import (
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/api/certificates/v1alpha1"
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
			controller.clustertrustbundles.Update(&v1alpha1.ClusterTrustBundle{
				ObjectMeta: v1.ObjectMeta{
					Name: istioClusterTrustBundleName,
				},
				Spec: v1alpha1.ClusterTrustBundleSpec{
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
