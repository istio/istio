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

package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/kclient"
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func trustDomainsCM(tds string) map[string]string {
	return map[string]string{constants.TrustDomainsNamespaceConfigMapDataName: tds}
}

func TestTrustDomainsController(t *testing.T) {
	client := kube.NewFakeClient()
	t.Cleanup(client.Shutdown)
	meshWatcher := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{
		TrustDomain:        "cluster.local",
		TrustDomainAliases: []string{"old.local"},
	})
	stop := test.NewStop(t)
	kube.SetObjectFilter(client, filter.NewDiscoveryNamespacesFilter(kclient.New[*v1.Namespace](client), meshWatcher, stop))
	c := NewTrustDomainsController(client, meshWatcher)
	client.RunAndWait(stop)
	go c.Run(stop)
	retry.UntilOrFail(t, c.queue.HasSynced)

	createNamespace(t, client.Kube(), "foo", nil)
	expectConfigMap(t, c.configmaps, TrustDomainsNamespaceConfigMap, "foo", trustDomainsCM("cluster.local\nold.local\n"))

	// Mesh config changes are written without a restart, including trust domains attached to a CA certificate.
	meshWatcher.Set(&meshconfig.MeshConfig{
		TrustDomain: "cluster.local",
		CaCertificates: []*meshconfig.MeshConfig_CertificateData{
			{TrustDomains: []string{"other.local", "cluster.local"}},
		},
	})
	expectConfigMap(t, c.configmaps, TrustDomainsNamespaceConfigMap, "foo", trustDomainsCM("cluster.local\nother.local\n"))

	// Tampering with the ConfigMap is reverted.
	_, err := client.Kube().CoreV1().ConfigMaps("foo").Update(context.TODO(), &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: TrustDomainsNamespaceConfigMap, Namespace: "foo"},
		Data:       trustDomainsCM("evil.local\n"),
	}, metav1.UpdateOptions{})
	assert.NoError(t, err)
	expectConfigMap(t, c.configmaps, TrustDomainsNamespaceConfigMap, "foo", trustDomainsCM("cluster.local\nother.local\n"))

	// Deletion is reverted.
	assert.NoError(t, client.Kube().CoreV1().ConfigMaps("foo").Delete(context.TODO(), TrustDomainsNamespaceConfigMap, metav1.DeleteOptions{}))
	expectConfigMap(t, c.configmaps, TrustDomainsNamespaceConfigMap, "foo", trustDomainsCM("cluster.local\nother.local\n"))

	for _, ns := range inject.IgnoredNamespaces.Copy().Delete(constants.KubeSystemNamespace).UnsortedList() {
		createNamespace(t, client.Kube(), ns, nil)
		err := retry.Until(func() bool {
			return c.configmaps.Get(TrustDomainsNamespaceConfigMap, ns) != nil
		}, retry.Timeout(time.Millisecond*25))
		if err == nil {
			t.Fatalf("%s namespace should not have %s configmap", ns, TrustDomainsNamespaceConfigMap)
		}
	}
}

func TestTrustDomainsDataSkipValidation(t *testing.T) {
	mesh := &meshconfig.MeshConfig{TrustDomain: "cluster.local", TrustDomainAliases: []string{"old.local"}}
	assert.Equal(t, string(trustDomainsData(mesh)), "cluster.local\nold.local\n")

	// Sidecars and waypoints accept any trust domain when validation is skipped; an empty list would instead
	// mean only the proxy's own trust domain.
	test.SetForTest(t, &features.SkipValidateTrustDomain, true)
	assert.Equal(t, string(trustDomainsData(mesh)), "*\n")
}

// The trust domains are needed whichever CA issues certificates, so they are written even when istiod does
// not distribute the CA certificate (external CA, cert-manager, SPIRE, ...).
func TestTrustDomainsWrittenWithoutCADistribution(t *testing.T) {
	clientset := kube.NewFakeClient()
	stop := test.NewStop(t)
	s := server.New()
	mcc := initController(clientset, stop)
	mockserviceController := newMockserviceController()
	_ = NewMulticluster("pilot-abc-123", Options{
		ClusterID:             cluster.ID("cluster-1"),
		DomainSuffix:          DomainSuffix,
		MeshWatcher:           meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{TrustDomain: "td.local"}),
		MeshNetworksWatcher:   meshwatcher.NewFixedNetworksWatcher(nil),
		MeshServiceController: mockserviceController,
	}, nil, nil, "default", false /* distributeCACert */, nil, s, mcc)
	assert.NoError(t, mcc.Run(stop))
	go mockserviceController.Run(stop)
	clientset.RunAndWait(stop)
	kube.WaitForCacheSync("test", stop, mcc.HasSynced)
	_ = s.Start(stop)

	createNamespace(t, clientset.Kube(), "foo", nil)
	expectTrustDomainsConfigMap(t, clientset, "foo", "td.local\n")
}

// waitForTrustDomainsController waits until the trust domains controller started by a Multicluster is writing.
func waitForTrustDomainsController(t *testing.T, client kube.Client) {
	t.Helper()
	createNamespace(t, client.Kube(), "trust-domains-probe", nil)
	expectTrustDomainsConfigMap(t, client, "trust-domains-probe", "")
}

// expectTrustDomainsConfigMap waits for the ConfigMap in ns; an empty want only waits for it to exist.
func expectTrustDomainsConfigMap(t *testing.T, client kube.Client, ns, want string) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		cm, err := client.Kube().CoreV1().ConfigMaps(ns).Get(context.TODO(), TrustDomainsNamespaceConfigMap, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if got := cm.Data[constants.TrustDomainsNamespaceConfigMapDataName]; want != "" && got != want {
			return fmt.Errorf("unexpected trust domains %q, want %q", got, want)
		}
		return nil
	}, retry.Timeout(10*time.Second))
}
