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
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/kclient"
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

func TestNamespaceController(t *testing.T) {
	client := kube.NewFakeClient()
	t.Cleanup(client.Shutdown)
	watcher := keycertbundle.NewWatcher()
	caBundle := []byte("caBundle")
	watcher.SetAndNotify(nil, nil, caBundle)
	meshWatcher := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{})
	stop := test.NewStop(t)
	discoveryNamespacesFilter := filter.NewDiscoveryNamespacesFilter(
		kclient.New[*v1.Namespace](client),
		meshWatcher,
		stop,
	)
	kube.SetObjectFilter(client, discoveryNamespacesFilter)
	nc := NewNamespaceController(client, watcher)
	client.RunAndWait(stop)
	go nc.Run(stop)
	retry.UntilOrFail(t, nc.queue.HasSynced)

	expectedData := map[string]string{
		constants.CACertNamespaceConfigMapDataName: string(caBundle),
	}
	createNamespace(t, client.Kube(), "foo", nil)
	expectConfigMap(t, nc.configmaps, CACertNamespaceConfigMap, "foo", expectedData)

	// Make sure random configmap does not get updated
	cmData := createConfigMap(t, client.Kube(), "not-root", "foo", "k")
	expectConfigMap(t, nc.configmaps, "not-root", "foo", cmData)

	newCaBundle := []byte("caBundle-new")
	watcher.SetAndNotify(nil, nil, newCaBundle)
	newData := map[string]string{
		constants.CACertNamespaceConfigMapDataName: string(newCaBundle),
	}
	expectConfigMap(t, nc.configmaps, CACertNamespaceConfigMap, "foo", newData)

	deleteConfigMap(t, client.Kube(), "foo")
	expectConfigMap(t, nc.configmaps, CACertNamespaceConfigMap, "foo", newData)

	ignoredNamespaces := inject.IgnoredNamespaces.Copy().Delete(constants.KubeSystemNamespace)
	for _, namespace := range ignoredNamespaces.UnsortedList() {
		// Create namespace in ignored list, make sure its not created
		createNamespace(t, client.Kube(), namespace, newData)
		// Configmap in that namespace should not do anything either
		createConfigMap(t, client.Kube(), "not-root", namespace, "k")
		expectConfigMapNotExist(t, nc.configmaps, namespace)
	}
}

func TestNamespaceControllerForCrlConfigMap(t *testing.T) {
	client := kube.NewFakeClient()
	t.Cleanup(client.Shutdown)
	watcher := keycertbundle.NewWatcher()
	crlData := []byte("crl")
	watcher.SetAndNotifyCACRL(crlData)
	meshWatcher := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{})
	stop := test.NewStop(t)
	discoveryNamespacesFilter := filter.NewDiscoveryNamespacesFilter(
		kclient.New[*v1.Namespace](client),
		meshWatcher,
		stop,
	)
	kube.SetObjectFilter(client, discoveryNamespacesFilter)
	nc := NewNamespaceController(client, watcher)
	client.RunAndWait(stop)
	go nc.Run(stop)
	retry.UntilOrFail(t, nc.queue.HasSynced)

	expectedData := map[string]string{
		constants.CACRLNamespaceConfigMapDataName: string(crlData),
	}
	createNamespace(t, client.Kube(), "foo", nil)
	expectConfigMap(t, nc.crlConfigmaps, CRLNamespaceConfigMap, "foo", expectedData)

	// Make sure random configmap does not get updated
	cmData := createConfigMap(t, client.Kube(), "not-root-cm", "foo", "key")
	expectConfigMap(t, nc.crlConfigmaps, "not-root-cm", "foo", cmData)

	newCrlData := []byte("new-crl")
	watcher.SetAndNotifyCACRL(newCrlData)
	newData := map[string]string{
		constants.CACRLNamespaceConfigMapDataName: string(newCrlData),
	}
	expectConfigMap(t, nc.crlConfigmaps, CRLNamespaceConfigMap, "foo", newData)

	deleteConfigMap(t, client.Kube(), "foo")
	expectConfigMap(t, nc.crlConfigmaps, CRLNamespaceConfigMap, "foo", newData)

	ignoredNamespaces := inject.IgnoredNamespaces.Copy().Delete(constants.KubeSystemNamespace)
	for _, namespace := range ignoredNamespaces.UnsortedList() {
		// Create namespace in ignored list, make sure it's not created
		createNamespace(t, client.Kube(), namespace, newData)
		// Configmap in that namespace should not do anything either
		createConfigMap(t, client.Kube(), "not-root-cm", namespace, "key")
		expectConfigMapNotExist(t, nc.crlConfigmaps, namespace)
	}
}

func TestNamespaceControllerWithDiscoverySelectors(t *testing.T) {
	client := kube.NewFakeClient()
	t.Cleanup(client.Shutdown)
	watcher := keycertbundle.NewWatcher()
	caBundle := []byte("caBundle")
	watcher.SetAndNotify(nil, nil, caBundle)
	meshWatcher := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{
		DiscoverySelectors: []*meshconfig.LabelSelector{
			{
				MatchLabels: map[string]string{
					"discovery-selectors": "enabled",
				},
			},
			{
				MatchExpressions: []*meshconfig.LabelSelectorRequirement{
					{
						Key:      "istio-tag",
						Operator: string(metav1.LabelSelectorOpNotIn),
						Values:   []string{"istio-canary", "istio-prod"},
					},
				},
			},
		},
	})
	stop := test.NewStop(t)
	discoveryNamespacesFilter := filter.NewDiscoveryNamespacesFilter(
		kclient.New[*v1.Namespace](client),
		meshWatcher,
		stop,
	)
	kube.SetObjectFilter(client, discoveryNamespacesFilter)
	nc := NewNamespaceController(client, watcher)
	client.RunAndWait(stop)
	go nc.Run(stop)
	retry.UntilOrFail(t, nc.queue.HasSynced)

	expectedData := map[string]string{
		constants.CACertNamespaceConfigMapDataName: string(caBundle),
	}
	testCases := []struct {
		name         string
		namespace    string
		labels       map[string]string
		expectConfig bool
	}{
		{
			name:         "Namespace with discovery selector enabled",
			namespace:    "nsA",
			labels:       map[string]string{"discovery-selectors": "enabled"},
			expectConfig: true,
		},
		{
			name:         "Namespace with istio-tag not in [istio-canary, istio-prod]",
			namespace:    "nsC",
			labels:       map[string]string{"istio-tag": "istio-dev"},
			expectConfig: true,
		},
		{
			name:         "Namespace with istio-tag in [istio-canary, istio-prod]",
			namespace:    "nsD",
			labels:       map[string]string{"istio-tag": "istio-canary"},
			expectConfig: false,
		},
		{
			name:         "Namespace with both discovery selector enabled and istio-tag not in [istio-canary, istio-prod]",
			namespace:    "nsE",
			labels:       map[string]string{"discovery-selectors": "enabled", "istio-tag": "istio-dev"},
			expectConfig: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			createNamespace(t, client.Kube(), tc.namespace, tc.labels)
			if tc.expectConfig {
				expectConfigMap(t, nc.configmaps, CACertNamespaceConfigMap, tc.namespace, expectedData)
			} else {
				expectConfigMapNotExist(t, nc.configmaps, tc.namespace)
			}
		})
	}
}

func TestNamespaceControllerDiscovery(t *testing.T) {
	client := kube.NewFakeClient()
	t.Cleanup(client.Shutdown)
	watcher := keycertbundle.NewWatcher()
	caBundle := []byte("caBundle")
	watcher.SetAndNotify(nil, nil, caBundle)
	meshWatcher := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{
		DiscoverySelectors: []*meshconfig.LabelSelector{{
			MatchLabels: map[string]string{"kubernetes.io/metadata.name": "selected"},
		}},
	})
	stop := test.NewStop(t)
	discoveryNamespacesFilter := filter.NewDiscoveryNamespacesFilter(
		kclient.New[*v1.Namespace](client),
		meshWatcher,
		stop,
	)
	kube.SetObjectFilter(client, discoveryNamespacesFilter)
	nc := NewNamespaceController(client, watcher)
	client.RunAndWait(stop)
	go nc.Run(stop)
	retry.UntilOrFail(t, nc.queue.HasSynced)

	expectedData := map[string]string{
		constants.CACertNamespaceConfigMapDataName: string(caBundle),
	}
	createNamespace(t, client.Kube(), "not-selected", map[string]string{"kubernetes.io/metadata.name": "not-selected"})
	createNamespace(t, client.Kube(), "selected", map[string]string{"kubernetes.io/metadata.name": "selected"})

	expectConfigMap(t, nc.configmaps, CACertNamespaceConfigMap, "selected", expectedData)
	expectConfigMapNotExist(t, nc.configmaps, "not-selected")

	meshWatcher.Set(&meshconfig.MeshConfig{
		DiscoverySelectors: []*meshconfig.LabelSelector{{
			MatchLabels: map[string]string{"kubernetes.io/metadata.name": "not-selected"},
		}},
	})
	expectConfigMap(t, nc.configmaps, CACertNamespaceConfigMap, "not-selected", expectedData)
	expectConfigMapNotExist(t, nc.configmaps, "selected")
}

// TestMultiRevisionNamespaceController verifies that two revisioned control planes with
// different discoverySelectors can independently create istio-ca-root-cert ConfigMaps
// for their respective namespaces. This is the scenario described in istio/istio#56594.
func TestMultiRevisionNamespaceController(t *testing.T) {
	// Use two separate kube clients (simulating two istiod processes) but sharing
	// the same underlying fake API server for namespaces.
	client1 := kube.NewFakeClient()
	t.Cleanup(client1.Shutdown)
	client2 := kube.NewFakeClient()
	t.Cleanup(client2.Shutdown)

	// Revision 1-22 selects namespaces with istio-discovery=istio-1-22
	watcher1 := keycertbundle.NewWatcher()
	caBundle1 := []byte("caBundle-rev122")
	watcher1.SetAndNotify(nil, nil, caBundle1)
	meshWatcher1 := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{
		DiscoverySelectors: []*meshconfig.LabelSelector{{
			MatchLabels: map[string]string{"istio-discovery": "istio-1-22"},
		}},
	})

	// Revision 1-23 selects namespaces with istio-discovery=istio-1-23
	watcher2 := keycertbundle.NewWatcher()
	caBundle2 := []byte("caBundle-rev123")
	watcher2.SetAndNotify(nil, nil, caBundle2)
	meshWatcher2 := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{
		DiscoverySelectors: []*meshconfig.LabelSelector{{
			MatchLabels: map[string]string{"istio-discovery": "istio-1-23"},
		}},
	})

	stop := test.NewStop(t)

	// Set up discovery filters per revision
	filter1 := filter.NewDiscoveryNamespacesFilter(
		kclient.New[*v1.Namespace](client1),
		meshWatcher1,
		stop,
	)
	kube.SetObjectFilter(client1, filter1)

	filter2 := filter.NewDiscoveryNamespacesFilter(
		kclient.New[*v1.Namespace](client2),
		meshWatcher2,
		stop,
	)
	kube.SetObjectFilter(client2, filter2)

	// Create namespace controllers per revision
	nc1 := NewNamespaceController(client1, watcher1)
	nc2 := NewNamespaceController(client2, watcher2)

	client1.RunAndWait(stop)
	client2.RunAndWait(stop)
	go nc1.Run(stop)
	go nc2.Run(stop)
	retry.UntilOrFail(t, nc1.queue.HasSynced)
	retry.UntilOrFail(t, nc2.queue.HasSynced)

	expectedData1 := map[string]string{
		constants.CACertNamespaceConfigMapDataName: string(caBundle1),
	}
	expectedData2 := map[string]string{
		constants.CACertNamespaceConfigMapDataName: string(caBundle2),
	}

	// Create namespaces for revision 1-22
	createNamespace(t, client1.Kube(), "httpbin122", map[string]string{
		"istio-discovery": "istio-1-22",
		"istio.io/rev":    "1-22",
	})
	createNamespace(t, client1.Kube(), "httpbin122-v2", map[string]string{
		"istio-discovery": "istio-1-22",
		"istio.io/rev":    "1-22",
	})

	// Create namespaces for revision 1-23
	createNamespace(t, client2.Kube(), "httpbin123", map[string]string{
		"istio-discovery": "istio-1-23",
		"istio.io/rev":    "1-23",
	})

	// Revision 1-22 should create ConfigMaps for its namespaces
	expectConfigMap(t, nc1.configmaps, CACertNamespaceConfigMap, "httpbin122", expectedData1)
	expectConfigMap(t, nc1.configmaps, CACertNamespaceConfigMap, "httpbin122-v2", expectedData1)

	// Revision 1-23 should create ConfigMaps for its namespaces
	expectConfigMap(t, nc2.configmaps, CACertNamespaceConfigMap, "httpbin123", expectedData2)

	// Revision 1-22 should NOT see namespaces for revision 1-23 (filtered out)
	expectConfigMapNotExist(t, nc1.configmaps, "httpbin123")

	// Revision 1-23 should NOT see namespaces for revision 1-22 (filtered out)
	expectConfigMapNotExist(t, nc2.configmaps, "httpbin122")
	expectConfigMapNotExist(t, nc2.configmaps, "httpbin122-v2")
}

func deleteConfigMap(t *testing.T, client kubernetes.Interface, ns string) {
	t.Helper()
	_, err := client.CoreV1().ConfigMaps(ns).Get(context.TODO(), CACertNamespaceConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CoreV1().ConfigMaps(ns).Delete(context.TODO(), CACertNamespaceConfigMap, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
}

func createConfigMap(t *testing.T, client kubernetes.Interface, name, ns, key string) map[string]string {
	t.Helper()
	data := map[string]string{key: "v"}
	_, err := client.CoreV1().ConfigMaps(ns).Create(context.Background(), &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Data: data,
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func createNamespace(t *testing.T, client kubernetes.Interface, ns string, labels map[string]string) {
	t.Helper()
	if _, err := client.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: labels},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func updateNamespace(t *testing.T, client kubernetes.Interface, ns string, labels map[string]string) {
	t.Helper()
	if _, err := client.CoreV1().Namespaces().Update(context.TODO(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: labels},
	}, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

// nolint:unparam
func expectConfigMap(t *testing.T, configmaps kclient.Client[*v1.ConfigMap], name, ns string, data map[string]string) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		cm := configmaps.Get(name, ns)
		if cm == nil {
			return fmt.Errorf("not found")
		}
		if !reflect.DeepEqual(cm.Data, data) {
			return fmt.Errorf("data mismatch, expected %+v got %+v", data, cm.Data)
		}
		return nil
	}, retry.Timeout(time.Second*10))
}

func expectConfigMapNotExist(t *testing.T, configmaps kclient.Client[*v1.ConfigMap], ns string) {
	t.Helper()
	err := retry.Until(func() bool {
		cm := configmaps.Get(CACertNamespaceConfigMap, ns)
		return cm != nil
	}, retry.Timeout(time.Millisecond*25))

	if err == nil {
		t.Fatalf("%s namespace should not have %s configmap.", ns, CACertNamespaceConfigMap)
	}
}
