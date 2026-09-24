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
	"sync/atomic"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	mcsapi "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/mcs"
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

func newMockserviceController() *aggregate.Controller {
	return aggregate.NewController(aggregate.Options{
		ConfigClusterID: "cluster-1",
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

// updateMultiClusterSecret rewrites the secret's kubeconfig bytes for cname, simulating
// credential rotation. The bytes must differ from the original so the sha256 check in
// addRemoteConfig doesn't treat this as a no-op.
func updateMultiClusterSecret(k8s kube.Client, sname, cname string, generation int) error {
	secret, err := k8s.Kube().CoreV1().Secrets(testSecretNameSpace).Get(context.TODO(), sname, metav1.GetOptions{})
	if err != nil {
		return err
	}
	secret.Data[cname] = []byte(fmt.Sprintf("Test-%d", generation))
	_, err = k8s.Kube().CoreV1().Secrets(testSecretNameSpace).Update(context.TODO(), secret, metav1.UpdateOptions{})
	return err
}

// registryForCluster finds the registered serviceregistry.Instance for a given cluster/provider,
// or nil if none is registered.
func registryForCluster(mockserviceController *aggregate.Controller, clusterID cluster.ID) serviceregistry.Instance {
	for _, r := range mockserviceController.GetRegistries() {
		if r.Cluster() == clusterID {
			return r
		}
	}
	return nil
}

func verifyControllers(t *testing.T, m *Multicluster, expectedControllerCount int, timeoutName string) {
	t.Helper()
	assert.EventuallyEqual(t, func() int {
		return len(m.component.All())
	}, expectedControllerCount, retry.Message(timeoutName), retry.Delay(time.Millisecond*10), retry.Timeout(time.Second*5))
}

func initController(client kube.CLIClient, stop <-chan struct{}) *multicluster.Controller {
	sc := multicluster.NewController(multicluster.ControllerOptions{
		Client:          client,
		SystemNamespace: testSecretNameSpace,
		ClusterID:       "cluster-1",
		MeshConfig:      meshwatcher.NewTestWatcher(nil),
		Debugger:        krt.GlobalDebugHandler,
	})
	sc.ClientBuilder = func(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		return kube.NewFakeClient(), nil
	}
	client.RunAndWait(stop)
	return sc
}

func Test_KubeSecretController(t *testing.T) {
	clusterID := cluster.ID("cluster-1")
	mockserviceController := newMockserviceController()
	clientset := kube.NewFakeClient()
	stop := test.NewStop(t)
	s := server.New()
	mcc := initController(clientset, stop)
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
		// handler semantics as the production code. So just spawn
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

// seedStableRemoteService creates a Service + EndpointSlice with a fixed IP on the given
// client. It is called from the ClientBuilder for every generation of a remote cluster's
// client (both the initial Add and every subsequent Update from a secret rotation), so the
// remote content is byte-for-byte identical across rotations - simulating a service whose
// endpoints do not change ("no pod churn") across a credential rotation.
func seedStableRemoteService(t *testing.T, client kube.Client, name, namespace, ip string) {
	t.Helper()
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1.ServiceSpec{
			ClusterIP:  ip,
			ClusterIPs: []string{ip},
			Ports:      []v1.ServicePort{{Name: "http", Port: 80, Protocol: v1.ProtocolTCP}},
		},
	}
	clienttest.NewWriter[*v1.Service](t, client).Create(svc)

	portName := "http"
	var portNum int32 = 80
	endpointIP := ip[:len(ip)-1] + "5" // distinct from the service ClusterIP
	slice := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{discovery.LabelServiceName: name},
		},
		AddressType: discovery.AddressTypeIPv4,
		Endpoints: []discovery.Endpoint{{
			Addresses: []string{endpointIP},
		}},
		Ports: []discovery.EndpointPort{{Name: &portName, Port: &portNum}},
	}
	clienttest.NewWriter[*discovery.EndpointSlice](t, client).Create(slice)
}

// seedStableRemoteServiceWithImport is seedStableRemoteService plus a ServiceImport, so a
// synthetic clusterset.local service is also generated for it.
func seedStableRemoteServiceWithImport(t *testing.T, client kube.Client, name, namespace, ip string, clusterSetVIPs []string) {
	t.Helper()
	clienttest.MakeCRD(t, client, mcs.ServiceImportGVR)
	seedStableRemoteService(t, client, name, namespace, ip)

	si := &mcsapi.ServiceImport{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceImport",
			APIVersion: "multicluster.x-k8s.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: mcsapi.ServiceImportSpec{
			Type: mcsapi.ClusterSetIP,
			IPs:  clusterSetVIPs,
		},
	}
	_, err := client.Dynamic().Resource(mcs.ServiceImportGVR).Namespace(namespace).Create(context.TODO(), toUnstructured(si), metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed creating ServiceImport: %v", err)
	}
}

// Test_KubeSecretController_CredentialRotation verifies that when a remote cluster's secret is
// updated with new kubeconfig bytes, the aggregate controller ends up with exactly one registry
// for that cluster at all times - the make-before-break swap in kubeController.HasSynced must
// complete before the old kubeController.Close() removes its registry, otherwise there would be
// a window where the cluster has no registered registry, or the swap would delete the
// newly-registered one out from under it.
//
// It also verifies that endpoint shards for services with stable (unchanged) endpoints in the
// remote cluster survive every rotation and that this holds for a service's synthetic MCS clusterset.local
// shard too.
func Test_KubeSecretController_CredentialRotation(t *testing.T) {
	test.SetForTest(t, &features.EnableMCSHost, true)

	const (
		remoteSvcName = "stable-svc"
		remoteSvcNS   = "app-ns"
		remoteSvcIP   = "10.10.0.1"
	)
	clusterSetVIPs := []string{"10.20.0.1"}
	hostname := remoteSvcName + "." + remoteSvcNS + ".svc." + DomainSuffix
	mcsHostname := string(serviceClusterSetLocalHostname(types.NamespacedName{Name: remoteSvcName, Namespace: remoteSvcNS}))

	clusterID := cluster.ID("cluster-1")
	remoteClusterID := cluster.ID("test-remote-cluster-1")
	shardKey := model.ShardKey{Cluster: remoteClusterID, Provider: provider.Kubernetes}

	endpointIndex := model.NewEndpointIndex(model.DisabledCache{})
	xdsUpdater := model.NewEndpointIndexUpdater(endpointIndex)

	mockserviceController := newMockserviceController()
	clientset := kube.NewFakeClient()
	stop := test.NewStop(t)
	s := server.New()
	mcc := initController(clientset, stop)
	mcc.ClientBuilder = func(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		remoteClient := kube.NewFakeClient()
		seedStableRemoteServiceWithImport(t, remoteClient, remoteSvcName, remoteSvcNS, remoteSvcIP, clusterSetVIPs)
		return remoteClient, nil
	}
	mc := NewMulticluster("pilot-abc-123", Options{
		ClusterID:             clusterID,
		DomainSuffix:          DomainSuffix,
		MeshWatcher:           meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{}),
		MeshNetworksWatcher:   meshwatcher.NewFixedNetworksWatcher(nil),
		MeshServiceController: mockserviceController,
		XDSUpdater:            xdsUpdater,
	}, nil, nil, "default", false, nil, s, mcc)
	assert.NoError(t, mcc.Run(stop))
	go mockserviceController.Run(stop)
	clientset.RunAndWait(stop)
	kube.WaitForCacheSync("test", stop, mcc.HasSynced)
	_ = s.Start(stop)

	verifyControllers(t, mc, 1, "create local controller")

	assert.NoError(t, createMultiClusterSecret(clientset, "test-secret-1", string(remoteClusterID)))
	verifyControllers(t, mc, 2, "create remote controller")

	retry.UntilSuccessOrFail(t, func() error {
		if registryForCluster(mockserviceController, remoteClusterID) == nil {
			return fmt.Errorf("expected a registry for %s after add", remoteClusterID)
		}
		return nil
	}, retry.Timeout(time.Second*5))
	originalRegistry := registryForCluster(mockserviceController, remoteClusterID)

	assertShardPopulated := func(hostname, when string) {
		t.Helper()
		retry.UntilSuccessOrFail(t, func() error {
			shards, ok := endpointIndex.ShardsForService(hostname, remoteSvcNS)
			if !ok {
				return fmt.Errorf("endpoint shard %v for %s missing %s", shardKey, hostname, when)
			}
			shards.RLock()
			defer shards.RUnlock()
			if len(shards.Shards[shardKey]) == 0 {
				return fmt.Errorf("endpoint shard %v for %s missing %s", shardKey, hostname, when)
			}
			return nil
		}, retry.Timeout(time.Second*5), retry.Delay(time.Millisecond*10))
	}
	assertShardPopulated(hostname, "after add")
	assertShardPopulated(mcsHostname, "after add")

	// Rotate the credentials a few times. After each rotation, the aggregate controller must have
	// exactly one registry for the cluster, and it must be a different instance than before -
	// never zero (a push/lookup gap) and never the stale one (a silently-dropped swap). The
	// remote cluster's content is unchanged across rotations (no pod churn), so its endpoint
	// shard (real and clusterset.local) must also survive every rotation.
	for generation := 1; generation <= 3; generation++ {
		assert.NoError(t, updateMultiClusterSecret(clientset, "test-secret-1", string(remoteClusterID), generation))

		retry.UntilSuccessOrFail(t, func() error {
			current := registryForCluster(mockserviceController, remoteClusterID)
			if current == nil {
				return fmt.Errorf("registry for %s missing after credential rotation %d", remoteClusterID, generation)
			}
			if current == originalRegistry {
				return fmt.Errorf("registry for %s was not swapped after credential rotation %d", remoteClusterID, generation)
			}
			return nil
		}, retry.Timeout(time.Second*5), retry.Delay(time.Millisecond*10))

		// The controller count must stay stable across the swap - no transient duplicate, no gap.
		verifyControllers(t, mc, 2, fmt.Sprintf("controller count after credential rotation %d", generation))
		assertShardPopulated(hostname, fmt.Sprintf("after credential rotation %d", generation))
		assertShardPopulated(mcsHostname, fmt.Sprintf("after credential rotation %d", generation))
		originalRegistry = registryForCluster(mockserviceController, remoteClusterID)
	}

	assert.NoError(t, deleteMultiClusterSecret(clientset, "test-secret-1"))
	verifyControllers(t, mc, 1, "delete remote controller")
}

// Test_KubeSecretController_CredentialRotation_StaleServiceAfterRotation validates a potential gap
// when a service is removed from the remote cluster in the same window as a credential rotation,
// its stale endpoint shard is never cleaned up.
//
// The old registry keeps running - and could observe the deletion itself - right up until the
// new registry syncs. But credential rotation is exactly the scenario where the old registry's
// watch connection can go bad (that's the reason for rotating), so it may never see the delete.
// The new registry's initial sync only lists what currently exists in the remote cluster, so a
// service that's already gone by then never generates any event to diff against - there's
// nothing to tell the shard index the old entry is now stale. kubeController.Close() no longer
// blindly wipes the shard for this exact reason (see the fix above), so that stale entry is
// never cleaned up.
func Test_KubeSecretController_CredentialRotation_StaleServiceAfterRotation(t *testing.T) {
	const (
		vanishingSvcName = "vanishing-svc"
		vanishingSvcNS   = "app-ns"
		vanishingSvcIP   = "10.10.0.2"
	)
	vanishingHostname := vanishingSvcName + "." + vanishingSvcNS + ".svc." + DomainSuffix

	clusterID := cluster.ID("cluster-1")
	remoteClusterID := cluster.ID("test-remote-cluster-1")
	shardKey := model.ShardKey{Cluster: remoteClusterID, Provider: provider.Kubernetes}

	endpointIndex := model.NewEndpointIndex(model.DisabledCache{})
	xdsUpdater := model.NewEndpointIndexUpdater(endpointIndex)

	mockserviceController := newMockserviceController()
	clientset := kube.NewFakeClient()
	stop := test.NewStop(t)
	s := server.New()
	mcc := initController(clientset, stop)

	// vanishing-svc is only ever seeded into the first generation of the remote client (the
	// initial Add) - simulating a service that existed when the cluster was first added, but
	// was deleted from the remote cluster before the credential rotation below, in the same
	// window the old registry's watch stopped observing changes.
	var generation atomic.Int32
	mcc.ClientBuilder = func(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		remoteClient := kube.NewFakeClient()
		if generation.Add(1) == 1 {
			seedStableRemoteService(t, remoteClient, vanishingSvcName, vanishingSvcNS, vanishingSvcIP)
		}
		return remoteClient, nil
	}
	mc := NewMulticluster("pilot-abc-123", Options{
		ClusterID:             clusterID,
		DomainSuffix:          DomainSuffix,
		MeshWatcher:           meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{}),
		MeshNetworksWatcher:   meshwatcher.NewFixedNetworksWatcher(nil),
		MeshServiceController: mockserviceController,
		XDSUpdater:            xdsUpdater,
	}, nil, nil, "default", false, nil, s, mcc)
	assert.NoError(t, mcc.Run(stop))
	go mockserviceController.Run(stop)
	clientset.RunAndWait(stop)
	kube.WaitForCacheSync("test", stop, mcc.HasSynced)
	_ = s.Start(stop)

	verifyControllers(t, mc, 1, "create local controller")
	assert.NoError(t, createMultiClusterSecret(clientset, "test-secret-1", string(remoteClusterID)))
	verifyControllers(t, mc, 2, "create remote controller")

	retry.UntilSuccessOrFail(t, func() error {
		shards, ok := endpointIndex.ShardsForService(vanishingHostname, vanishingSvcNS)
		if !ok {
			return fmt.Errorf("expected endpoint shard for %s to be populated before rotation", vanishingHostname)
		}
		shards.RLock()
		defer shards.RUnlock()
		if len(shards.Shards[shardKey]) == 0 {
			return fmt.Errorf("expected endpoint shard for %s to be populated before rotation", vanishingHostname)
		}
		return nil
	}, retry.Timeout(time.Second*5), retry.Delay(time.Millisecond*10))

	originalRegistry := registryForCluster(mockserviceController, remoteClusterID)

	// Rotate credentials. The remote cluster's fixture no longer includes vanishing-svc,
	// simulating that it was deleted before this rotation - the new registry's fresh sync
	// never lists it, so nothing ever generates a delete for the shard entry the old registry
	// wrote.
	assert.NoError(t, updateMultiClusterSecret(clientset, "test-secret-1", string(remoteClusterID), 1))
	retry.UntilSuccessOrFail(t, func() error {
		current := registryForCluster(mockserviceController, remoteClusterID)
		if current == nil || current == originalRegistry {
			return fmt.Errorf("registry for %s not swapped after credential rotation", remoteClusterID)
		}
		return nil
	}, retry.Timeout(time.Second*5), retry.Delay(time.Millisecond*10))

	// The deleted service's endpoint shard must eventually be cleaned up - it must not serve
	// stale endpoints for a service that no longer exists in the remote cluster.
	retry.UntilSuccessOrFail(t, func() error {
		shards, ok := endpointIndex.ShardsForService(vanishingHostname, vanishingSvcNS)
		if !ok {
			return nil
		}
		shards.RLock()
		defer shards.RUnlock()
		if len(shards.Shards[shardKey]) == 0 {
			return nil
		}
		return fmt.Errorf("stale endpoint shard %v for deleted service %s still present after rotation", shardKey, vanishingHostname)
	}, retry.Timeout(time.Second*2), retry.Delay(time.Millisecond*10))

	assert.NoError(t, deleteMultiClusterSecret(clientset, "test-secret-1"))
	verifyControllers(t, mc, 1, "delete remote controller")
}

func Test_KubeSecretController_ExternalIstiod_MultipleClusters(t *testing.T) {
	test.SetForTest(t, &features.ExternalIstiod, true)
	test.SetForTest(t, &features.InjectionWebhookConfigName, "")
	clusterID := cluster.ID("cluster-1")
	mockserviceController := newMockserviceController()
	clientset := kube.NewFakeClient()
	stop := test.NewStop(t)
	s := server.New()
	certWatcher := keycertbundle.NewWatcher()
	mcc := initController(clientset, stop)
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
