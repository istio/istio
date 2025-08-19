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
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/util/leak"
)

const (
	testSecretNameSpace = "istio-system"
	DomainSuffix        = "fake_domain"
)

func newMockserviceController(configCluster cluster.ID) *aggregate.Controller {
	return aggregate.NewController(aggregate.Options{
		ConfigClusterID: configCluster,
	})
}

func createMultiClusterSecret(k8s kube.Client, sname, cname string) error {
	data := map[string][]byte{}
	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sname,
			Namespace: testSecretNameSpace,
			Labels: map[string]string{
				multicluster.MultiClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{},
	}

	data[cname] = []byte("Test")
	secret.Data = data
	_, err := k8s.Kube().CoreV1().Secrets(testSecretNameSpace).Create(context.TODO(), &secret, metav1.CreateOptions{})
	return err
}

func deleteMultiClusterSecret(k8s kube.Client, sname string) error {
	var immediate int64

	return k8s.Kube().CoreV1().Secrets(testSecretNameSpace).Delete(
		context.TODO(),
		sname, metav1.DeleteOptions{GracePeriodSeconds: &immediate})
}

func verifyControllers(t *testing.T, m *Multicluster, expectedControllerCount int, timeoutName string) {
	t.Helper()
	assert.EventuallyEqual(t, func() int {
		return len(m.component.All())
	}, expectedControllerCount, retry.Message(timeoutName), retry.Delay(time.Millisecond*10), retry.Timeout(time.Second*5))
}

func initController(client kube.CLIClient, ns string, stop <-chan struct{}) *multicluster.Controller {
	sc := multicluster.NewController(client, ns, "cluster-1", meshwatcher.NewTestWatcher(nil))
	sc.ClientBuilder = func(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		return kube.NewFakeClient(), nil
	}
	client.RunAndWait(stop)
	return sc
}

func Test_KubeSecretController(t *testing.T) {
	clusterID := cluster.ID("cluster-1")
	mockserviceController := newMockserviceController(clusterID)
	clientset := kube.NewFakeClient()
	stop := test.NewStop(t)
	s := server.New()
	mcc := initController(clientset, testSecretNameSpace, stop)
	mc := NewMulticluster("pilot-abc-123", Options{
		ClusterID:    clusterID,
		DomainSuffix: DomainSuffix,
		MeshWatcher:  meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{}),
		// Added to better simulate a real environment and keep the goroutine leak test honest
		MeshNetworksWatcher:   meshwatcher.NewFixedNetworksWatcher(nil),
		MeshServiceController: mockserviceController,
	}, nil, nil, "default", false, nil, s, mcc)
	assert.NoError(t, mcc.Run(stop))
	go mockserviceController.Run(stop)
	clientset.RunAndWait(stop)
	kube.WaitForCacheSync("test", stop, mcc.HasSynced)
	_ = s.Start(stop)

	verifyControllers(t, mc, 1, "create local controller")
	t.Run("multicluster secret added", func(t *testing.T) {
		// Verify that we only leaked the expected number of goroutines.
		// 1. MeshNetworks event handler for the remote cluster
		// 2. MeshConfig event handler for the remote cluster
		// Unfortunately, the test versions of these singletons
		// use static collections which don't have the same event
		// handler semantics as the prodcution code. So just spawn
		// two goroutines to simulate the leak.
		stop = test.NewStop(t)
		leak.Check(t, leak.WithAllowedLeaks(2))
		// TODO: Remove if we ever make static collections concurrent
		go func() {
			<-stop
		}()
		go func() {
			<-stop
		}()
		// Create the multicluster secret. Sleep to allow created remote
		// controller to start and callback add function to be called.
		err := createMultiClusterSecret(clientset, "test-secret-1", "test-remote-cluster-1")
		if err != nil {
			t.Fatalf("Unexpected error on secret create: %v", err)
		}

		// Test - Verify that the remote controller has been added.
		verifyControllers(t, mc, 2, "create remote controller")

		// Delete the mulicluster secret.
		err = deleteMultiClusterSecret(clientset, "test-secret-1")
		if err != nil {
			t.Fatalf("Unexpected error on secret delete: %v", err)
		}

		// Test - Verify that the remote controller has been removed.
		verifyControllers(t, mc, 1, "delete remote controller")
	})
}

func Test_KubeSecretController_ExternalIstiod_MultipleClusters(t *testing.T) {
	test.SetForTest(t, &features.ExternalIstiod, true)
	test.SetForTest(t, &features.InjectionWebhookConfigName, "")
	clusterID := cluster.ID("cluster-1")
	mockserviceController := newMockserviceController(clusterID)
	clientset := kube.NewFakeClient()
	stop := test.NewStop(t)
	s := server.New()
	certWatcher := keycertbundle.NewWatcher()
	mcc := initController(clientset, testSecretNameSpace, stop)
	mc := NewMulticluster("pilot-abc-123", Options{
		ClusterID:             clusterID,
		DomainSuffix:          DomainSuffix,
		MeshWatcher:           meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{}),
		MeshServiceController: mockserviceController,
	}, nil, certWatcher, "default", false, nil, s, mcc)
	assert.NoError(t, mcc.Run(stop))
	go mockserviceController.Run(stop)
	clientset.RunAndWait(stop)
	kube.WaitForCacheSync("test", stop, mcc.HasSynced)
	_ = s.Start(stop)

	// the multicluster controller will register the local cluster
	verifyControllers(t, mc, 1, "registered local cluster controller")

	// Create the multicluster secret. Sleep to allow created remote
	// controller to start and callback add function to be called.
	err := createMultiClusterSecret(clientset, "test-secret-1", "test-remote-cluster-1")
	if err != nil {
		t.Fatalf("Unexpected error on secret create: %v", err)
	}

	// Test - Verify that the remote controller has been added.
	verifyControllers(t, mc, 2, "create remote controller 1")

	// Create second multicluster secret. Sleep to allow created remote
	// controller to start and callback add function to be called.
	err = createMultiClusterSecret(clientset, "test-secret-2", "test-remote-cluster-2")
	if err != nil {
		t.Fatalf("Unexpected error on secret create: %v", err)
	}

	// Test - Verify that the remote controller has been added.
	verifyControllers(t, mc, 3, "create remote controller 2")

	// Delete the first mulicluster secret.
	err = deleteMultiClusterSecret(clientset, "test-secret-1")
	if err != nil {
		t.Fatalf("Unexpected error on secret delete: %v", err)
	}

	// Test - Verify that the remote controller has been removed.
	verifyControllers(t, mc, 2, "delete remote controller 1")

	// Delete the second mulicluster secret.
	err = deleteMultiClusterSecret(clientset, "test-secret-2")
	if err != nil {
		t.Fatalf("Unexpected error on secret delete: %v", err)
	}

	// Test - Verify that the remote controller has been removed.
	verifyControllers(t, mc, 1, "delete remote controller 2")
}
