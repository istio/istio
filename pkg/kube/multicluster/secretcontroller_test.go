// Copyright Istio Authors.
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

package multicluster

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	uberatomic "go.uber.org/atomic"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

const secretNamespace string = "istio-system"

type clusterCredential struct {
	clusterID  string
	kubeconfig []byte
}

func makeSecret(namespace string, secret string, clusterConfigs ...clusterCredential) *v1.Secret {
	s := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret,
			Namespace: namespace,
			Labels: map[string]string{
				MultiClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{},
	}

	for _, config := range clusterConfigs {
		s.Data[config.clusterID] = config.kubeconfig
	}
	return s
}

func TestKubeConfigOverride(t *testing.T) {
	var (
		expectedQPS   = float32(100)
		expectedBurst = 200
	)
	fakeRestConfig := &rest.Config{}
	client := kube.NewFakeClient()
	stopCh := test.NewStop(t)
	c := NewController(client, secretNamespace, "", meshwatcher.NewTestWatcher(nil), func(cfg *rest.Config) {
		cfg.QPS = expectedQPS
		cfg.Burst = expectedBurst
	})
	c.ClientBuilder = func(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		for _, override := range configOverrides {
			override(fakeRestConfig)
		}
		return kube.NewFakeClient(), nil
	}
	client.RunAndWait(stopCh)
	assert.NoError(t, c.Run(stopCh))
	retry.UntilOrFail(t, c.HasSynced, retry.Timeout(2*time.Second))
	secret0 := makeSecret(secretNamespace, "s0", clusterCredential{"c0", []byte("kubeconfig0-0")})
	secrets := clienttest.NewWriter[*v1.Secret](t, client)
	t.Run("test kube config override", func(t *testing.T) {
		secrets.Create(secret0)
		assert.EventuallyEqual(t, func() bool {
			return c.cs.GetByID("c0") != nil
		}, true)
		assert.Equal(t, fakeRestConfig, &rest.Config{
			QPS:   expectedQPS,
			Burst: expectedBurst,
		})
	})
}

func TestingBuildClientsFromConfig(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
	return kube.NewFakeClient(), nil
}

type testController struct {
	controller *Controller
	client     kube.Client
	component  *Component[testHandler]
	t          *testing.T
	secrets    clienttest.TestWriter[*v1.Secret]
}

func buildTestController(t *testing.T, synced bool) testController {
	tc := testController{
		client: kube.NewFakeClient(),
		t:      t,
	}
	tc.secrets = clienttest.NewWriter[*v1.Secret](t, tc.client)
	tc.controller = NewController(tc.client, secretNamespace, "config", meshwatcher.NewTestWatcher(nil))
	tc.controller.ClientBuilder = TestingBuildClientsFromConfig
	iter := uberatomic.NewInt32(0)
	tc.component = BuildMultiClusterComponent(tc.controller, func(cluster *Cluster) testHandler {
		it := iter.Inc()
		return testHandler{
			ID:     cluster.ID,
			Iter:   int(it),
			Closed: uberatomic.NewBool(false),
			Synced: uberatomic.NewBool(synced),
		}
	})
	return tc
}

var kubeconfig = 0

func (t *testController) AddSecret(secretName, clusterID string) {
	kubeconfig++
	t.secrets.CreateOrUpdate(makeSecret(secretNamespace, secretName, clusterCredential{clusterID, []byte(fmt.Sprintf("kubeconfig-%d", kubeconfig))}))
}

func (t *testController) DeleteSecret(secretName string) {
	t.t.Helper()
	t.secrets.Delete(secretName, secretNamespace)
}

func (t *testController) ConfigClusterHandler() testHandler {
	return t.component.clusters["config"]
}

func (t *testController) Run(stop chan struct{}) {
	assert.NoError(t.t, t.controller.Run(stop))
	t.client.RunAndWait(stop)
}

func TestListRemoteClusters(t *testing.T) {
	stop := make(chan struct{})
	c := buildTestController(t, false)
	c.AddSecret("s0", "c0")
	c.AddSecret("s1", "c1")
	c.Run(stop)

	// before sync
	assert.EventuallyEqual(t, c.controller.ListRemoteClusters, []cluster.DebugInfo{
		{ID: "config", SyncStatus: SyncStatusSyncing},
		{ID: "c0", SecretName: "istio-system/s0", SyncStatus: SyncStatusSyncing},
		{ID: "c1", SecretName: "istio-system/s1", SyncStatus: SyncStatusSyncing},
	})
	assert.EventuallyEqual(t, func() int { return len(c.component.All()) }, 3)

	// Sync all but one
	for _, c := range c.component.All() {
		if c.ID != "c1" {
			c.Synced.Store(true)
		}
	}
	assert.EventuallyEqual(t, c.controller.ListRemoteClusters, []cluster.DebugInfo{
		{ID: "config", SyncStatus: SyncStatusSynced},
		{ID: "c0", SecretName: "istio-system/s0", SyncStatus: SyncStatusSynced},
		{ID: "c1", SecretName: "istio-system/s1", SyncStatus: SyncStatusSyncing},
	})

	// Sync the last one
	c.component.ForCluster("c1").Synced.Store(true)
	assert.EventuallyEqual(t, c.controller.ListRemoteClusters, []cluster.DebugInfo{
		{ID: "config", SyncStatus: SyncStatusSynced},
		{ID: "c0", SecretName: "istio-system/s0", SyncStatus: SyncStatusSynced},
		{ID: "c1", SecretName: "istio-system/s1", SyncStatus: SyncStatusSynced},
	})

	// Remove one
	c.DeleteSecret("s1")
	assert.EventuallyEqual(t, c.controller.ListRemoteClusters, []cluster.DebugInfo{
		{ID: "config", SyncStatus: SyncStatusSynced},
		{ID: "c0", SecretName: "istio-system/s0", SyncStatus: SyncStatusSynced},
	})
}

func TestShutdown(t *testing.T) {
	stop := make(chan struct{})
	c := buildTestController(t, true)
	c.AddSecret("s0", "c0")
	c.AddSecret("s1", "c1")
	c.Run(stop)
	retry.UntilOrFail(t, c.controller.HasSynced, retry.Timeout(2*time.Second))
	components := c.component.All()
	assert.Equal(t, []bool{false, false, false}, slices.Map(components, func(e testHandler) bool {
		return e.Closed.Load()
	}))

	// Remove secret, it should be marked as closed
	c.DeleteSecret("s0")
	fetchClosed := func() map[string]bool {
		res := map[string]bool{}
		for _, c := range components {
			res[string(c.ID)] = c.Closed.Load()
		}
		return res
	}
	assert.EventuallyEqual(t, fetchClosed, map[string]bool{"config": false, "c1": false, "c0": true})

	// close everything
	close(stop)

	// We should *not* shutdown anything else except the config cluster
	// In theory we could, but we only shut down the controller when the entire application is closing so we don't bother
	assert.EventuallyEqual(t, fetchClosed, map[string]bool{"config": true, "c1": false, "c0": true})
}

// TestObjectFilter tests that when a component is created, it should have access to the objectfilter.
// This ensures we do not load everything, then later filter it out.
func TestObjectFilter(t *testing.T) {
	stop := test.NewStop(t)
	clientWithNamespace := func() kube.Client {
		return kube.NewFakeClient(
			&v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "allowed",
					Labels: map[string]string{"kubernetes.io/metadata.name": "allowed"},
				},
			},
			&v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "not-allowed",
					Labels: map[string]string{"kubernetes.io/metadata.name": "not-allowed"},
				},
			})
	}
	tc := testController{
		client: clientWithNamespace(),
		t:      t,
	}
	mesh := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{
		DiscoverySelectors: []*meshconfig.LabelSelector{
			{
				MatchLabels: map[string]string{
					"kubernetes.io/metadata.name": "allowed",
				},
			},
		},
	})

	clusterID := cluster.ID("config")
	// For primary cluster, we need to set it up ourselves.
	namespaces := kclient.New[*v1.Namespace](tc.client)
	filter := namespace.NewDiscoveryNamespacesFilter(namespaces, mesh, stop)
	tc.client = kube.SetObjectFilter(tc.client, filter)

	tc.secrets = clienttest.NewWriter[*v1.Secret](t, tc.client)
	tc.controller = NewController(tc.client, secretNamespace, clusterID, mesh)
	tc.controller.ClientBuilder = func(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		return clientWithNamespace(), nil
	}

	tc.component = BuildMultiClusterComponent(tc.controller, func(cluster *Cluster) testHandler {
		// Filter must immediately work!
		assert.Equal(t, cluster.Client.ObjectFilter() != nil, true, "cluster "+cluster.ID.String())
		assert.Equal(t, cluster.Client.ObjectFilter().Filter("allowed"), true)
		assert.Equal(t, cluster.Client.ObjectFilter().Filter("not-allowed"), false)
		return testHandler{
			ID:     cluster.ID,
			Closed: uberatomic.NewBool(false),
			Synced: uberatomic.NewBool(true),
		}
	})

	tc.AddSecret("s0", "c0")
	tc.AddSecret("s1", "c1")
	tc.Run(stop)
	retry.UntilOrFail(t, tc.controller.HasSynced, retry.Timeout(2*time.Second))
}

type informerHandler[T controllers.ComparableObject] struct {
	client kclient.Client[T]
}

func (i *informerHandler[T]) Close() {
	i.client.ShutdownHandlers()
}

func (i *informerHandler[T]) HasSynced() bool {
	return i.client.HasSynced()
}

// Test our (lack of) ability to do seamless updates of a cluster.
// Tracking improvements in https://github.com/istio/istio/issues/49349
func TestSeamlessMigration(t *testing.T) {
	stop := make(chan struct{})
	c := buildTestController(t, true)
	tt := assert.NewTracker[string](t)
	initial := kube.NewFakeClient(
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "initial"},
		},
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "common"},
		},
	)
	later := kube.NewFakeClient(
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "later"},
		},
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "common"},
		},
	)
	nextClient := initial
	c.controller.ClientBuilder = func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		ret := nextClient
		nextClient = later
		return ret, nil
	}
	component := BuildMultiClusterComponent(c.controller, func(cluster *Cluster) *informerHandler[*v1.ConfigMap] {
		cl := kclient.New[*v1.ConfigMap](cluster.Client)
		trackerHandler := clienttest.TrackerHandler(tt)
		var handler cache.ResourceEventHandler = trackerHandler
		// If this is an update and we have a previous component, use seamless migration
		if cluster.Action == Update && cluster.prevComponent != nil {
			if oldHandler, ok := cluster.prevComponent.(*informerHandler[*v1.ConfigMap]); ok {
				handler = wrappedEventHandler(
					oldHandler.client,
					cl,
					trackerHandler,
					cluster.stop,
				)
			}
		}
		cl.AddEventHandler(handler)
		return &informerHandler[*v1.ConfigMap]{client: cl}
	})
	c.AddSecret("s0", "c0")
	c.Run(stop)
	retry.UntilOrFail(t, c.controller.HasSynced, retry.Timeout(2*time.Second))
	assert.Equal(t,
		clienttest.Names((*component.ForCluster("c0")).client.List(metav1.NamespaceAll, klabels.Everything())),
		sets.New("initial", "common"))

	tt.WaitUnordered("add/common", "add/initial")

	// Update the cluster
	c.AddSecret("s0", "c0")
	var fatal error
	retry.UntilOrFail(t, func() bool {
		have := clienttest.Names((*component.ForCluster("c0")).client.List(metav1.NamespaceAll, klabels.Everything()))
		if have.Equals(sets.New("later", "common")) {
			return true
		}
		if !have.Equals(sets.New("initial", "common")) {
			fatal = fmt.Errorf("unexpected contents: %v", have)
			// Return true immediately to fail fast if we see an unexpected state.
			// This ensures we do not go from `old -> empty -> new` and instead go from `old -> new` seamlessly.
			return true
		}
		return false
	})
	// Verify we never saw an unexpected state (like empty) during the transition
	if fatal != nil {
		t.Fatalf("should not see empty or unexpected state during cluster update: %v", fatal)
	}
	// With seamless migration implemented, we should get 'delete/initial, add/later, update/common'.
	// The test should fail until seamless migration properly implements UPDATE events for objects that exist in both old and new clusters.
	// See discussion in https://github.com/istio/enhancements/pull/107
	tt.WaitUnordered("delete/initial", "add/later", "update/common")
}

func TestSecretController(t *testing.T) {
	client := kube.NewFakeClient()

	var (
		secret0 = makeSecret(secretNamespace, "s0",
			clusterCredential{"c0", []byte("kubeconfig0-0")})
		secret0UpdateKubeconfigChanged = makeSecret(secretNamespace, "s0",
			clusterCredential{"c0", []byte("kubeconfig0-1")})
		secret0UpdateKubeconfigSame = makeSecret(secretNamespace, "s0",
			clusterCredential{"c0", []byte("kubeconfig0-1")})
		secret0AddCluster = makeSecret(secretNamespace, "s0",
			clusterCredential{"c0", []byte("kubeconfig0-1")}, clusterCredential{"c0-1", []byte("kubeconfig0-2")})
		secret0DeleteCluster = secret0UpdateKubeconfigChanged // "c0-1" cluster deleted
		secret0ReAddCluster  = makeSecret(secretNamespace, "s0",
			clusterCredential{"c0", []byte("kubeconfig0-1")}, clusterCredential{"c0-1", []byte("kubeconfig0-2")})
		secret0ReDeleteCluster = secret0UpdateKubeconfigChanged // "c0-1" cluster re-deleted
		secret1                = makeSecret(secretNamespace, "s1",
			clusterCredential{"c1", []byte("kubeconfig1-0")})
		otherNSSecret = makeSecret("some-other-namespace", "s2",
			clusterCredential{"c1", []byte("kubeconfig1-0")})
		secret2Cluster0 = makeSecret(secretNamespace, "s2",
			clusterCredential{"c0", []byte("kubeconfig1-1")})
		configCluster = makeSecret(secretNamespace, "s3",
			clusterCredential{"config", []byte("kubeconfig3-0")})
	)

	secret0UpdateKubeconfigSame.Annotations = map[string]string{"foo": "bar"}

	type result struct {
		ID   cluster.ID
		Iter int
	}

	steps := []struct {
		name string
		// only set one of these per step. The others should be nil.
		add    *v1.Secret
		update *v1.Secret
		delete *v1.Secret

		want []result
	}{
		{
			name: "Create secret s0 and add kubeconfig for cluster c0, which will add remote cluster c0",
			add:  secret0,
			want: []result{{"config", 1}, {"c0", 2}},
		},
		{
			name:   "Update secret s0 and update the kubeconfig of cluster c0, which will update remote cluster c0",
			update: secret0UpdateKubeconfigChanged,
			want:   []result{{"config", 1}, {"c0", 3}},
		},
		{
			name:   "Update secret s0 but keep the kubeconfig of cluster c0 unchanged, which will not update remote cluster c0",
			update: secret0UpdateKubeconfigSame,
			want:   []result{{"config", 1}, {"c0", 3}},
		},
		{
			name: "Update secret s0 and add kubeconfig for cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will add remote cluster c0-1 but will not update remote cluster c0",
			update: secret0AddCluster,
			want:   []result{{"config", 1}, {"c0", 3}, {"c0-1", 4}},
		},
		{
			name: "Update secret s0 and delete cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will delete remote cluster c0-1 but will not update remote cluster c0",
			update: secret0DeleteCluster,
			want:   []result{{"config", 1}, {"c0", 3}},
		},
		{
			name: "Update secret s0 and re-add kubeconfig for cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will add remote cluster c0-1 but will not update remote cluster c0",
			update: secret0ReAddCluster,
			want:   []result{{"config", 1}, {"c0", 3}, {"c0-1", 5}},
		},
		{
			name: "Update secret s0 and re-delete cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
				"which will delete remote cluster c0-1 but will not update remote cluster c0",
			update: secret0ReDeleteCluster,
			want:   []result{{"config", 1}, {"c0", 3}},
		},
		{
			name: "Create secret s1 and add kubeconfig for cluster c1, which will add remote cluster c1",
			add:  secret1,
			want: []result{{"config", 1}, {"c0", 3}, {"c1", 6}},
		},
		{
			name: "Add secret s2, with already existing cluster",
			add:  secret2Cluster0,
			want: []result{{"config", 1}, {"c0", 3}, {"c1", 6}},
		},
		{
			name:   "Delete secret s2, with already existing cluster",
			delete: secret2Cluster0,
			want:   []result{{"config", 1}, {"c0", 3}, {"c1", 6}},
		},
		{
			name:   "Delete secret s0, which will delete remote cluster c0",
			delete: secret0,
			want:   []result{{"config", 1}, {"c1", 6}},
		},
		{
			name:   "Delete secret s1, which will delete remote cluster c1",
			delete: secret1,
			want:   []result{{"config", 1}},
		},
		{
			name: "Add secret from another namespace",
			add:  otherNSSecret,
			want: []result{{"config", 1}},
		},
		{
			name: "Add secret referencing config cluster",
			add:  configCluster,
			want: []result{{"config", 1}},
		},
		{
			name:   "Delete secret referencing config cluster",
			delete: configCluster,
			want:   []result{{"config", 1}},
		},
	}

	// Start the secret controller and sleep to allow secret process to start.
	stopCh := test.NewStop(t)
	c := NewController(client, secretNamespace, "config", meshwatcher.NewTestWatcher(nil))
	c.ClientBuilder = TestingBuildClientsFromConfig
	client.RunAndWait(stopCh)
	secrets := clienttest.NewWriter[*v1.Secret](t, client)
	iter := 0
	component := BuildMultiClusterComponent(c, func(cluster *Cluster) testHandler {
		iter++
		return testHandler{
			ID:     cluster.ID,
			Iter:   iter,
			Closed: uberatomic.NewBool(false),
			Synced: uberatomic.NewBool(false),
		}
	})
	client.RunAndWait(stopCh)
	assert.NoError(t, c.Run(stopCh))
	// Should not be synced...
	assert.Equal(t, c.HasSynced(), false)
	// Now mark the config cluster as synced
	component.All()[0].Synced.Store(true)
	t.Run("sync timeout", func(t *testing.T) {
		retry.UntilOrFail(t, c.HasSynced, retry.Timeout(2*time.Second))
	})
	kube.WaitForCacheSync("test", stopCh, c.HasSynced)

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			switch {
			case step.add != nil:
				secrets.Create(step.add)
			case step.update != nil:
				secrets.Update(step.update)
			case step.delete != nil:
				secrets.Delete(step.delete.Name, step.delete.Namespace)
			}

			assert.EventuallyEqual(t, func() []result {
				return slices.Map(component.All(), func(e testHandler) result {
					return result{e.ID, e.Iter}
				})
			}, step.want)
		})
	}
}

type testHandler struct {
	ID     cluster.ID
	Iter   int
	Closed *uberatomic.Bool
	Synced *uberatomic.Bool
}

func (h testHandler) Close() {
	h.Closed.Store(true)
}

func (h testHandler) HasSynced() bool {
	return h.Synced.Load()
}

// TestClusterUpdateHotSwap verifies that during credential rotation:
// 1. The old cluster continues serving until the new one syncs
// 2. There's no gap where services are unavailable
// 3. The old component is only closed after the new one is synced
func TestClusterUpdateHotSwap(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)

	client := kube.NewFakeClient()
	secrets := clienttest.NewWriter[*v1.Secret](t, client)

	c := NewController(client, secretNamespace, "config", meshwatcher.NewTestWatcher(nil))
	c.ClientBuilder = TestingBuildClientsFromConfig

	// Track component lifecycle events
	type lifecycleEvent struct {
		action    string // "created" or "closed"
		clusterID cluster.ID
		iter      int
	}
	events := make(chan lifecycleEvent, 100)

	iter := uberatomic.NewInt32(0)
	component := BuildMultiClusterComponent(c, func(cluster *Cluster) testHandler {
		it := int(iter.Inc())
		events <- lifecycleEvent{"created", cluster.ID, it}
		return testHandler{
			ID:     cluster.ID,
			Iter:   it,
			Closed: uberatomic.NewBool(false),
			Synced: uberatomic.NewBool(true), // Auto-sync for simplicity
		}
	})

	client.RunAndWait(stop)
	assert.NoError(t, c.Run(stop))
	retry.UntilOrFail(t, c.HasSynced, retry.Timeout(2*time.Second))

	// Drain the config cluster creation event
	<-events

	// Create initial cluster
	secret0 := makeSecret(secretNamespace, "s0", clusterCredential{"c0", []byte("kubeconfig-initial")})
	secrets.Create(secret0)

	// Wait for cluster to be created
	assert.EventuallyEqual(t, func() bool {
		return c.cs.GetByID("c0") != nil
	}, true)
	<-events // drain "created" event

	// Get the initial handler
	initialHandler := component.ForCluster("c0")
	assert.Equal(t, initialHandler != nil, true)
	initialIter := initialHandler.Iter

	// Update the secret (credential rotation)
	secret0Updated := makeSecret(secretNamespace, "s0", clusterCredential{"c0", []byte("kubeconfig-rotated")})
	secrets.Update(secret0Updated)

	// Wait for new component to be created
	var newCreatedEvent lifecycleEvent
	assert.EventuallyEqual(t, func() bool {
		select {
		case e := <-events:
			if e.action == "created" && e.clusterID == "c0" {
				newCreatedEvent = e
				return true
			}
		default:
		}
		return false
	}, true)

	// Verify new component was created with higher iteration
	assert.Equal(t, newCreatedEvent.iter > initialIter, true)

	// The new handler should now be in place
	newHandler := component.ForCluster("c0")
	assert.Equal(t, newHandler != nil, true)
	assert.Equal(t, newHandler.Iter, newCreatedEvent.iter)

	// Wait a bit and verify the old component was closed (via pendingSwap.HasSynced)
	// Since we set Synced to true automatically, the old should be closed
	retry.UntilOrFail(t, func() bool {
		return initialHandler.Closed.Load()
	}, retry.Timeout(2*time.Second))

	// Verify the new handler is still active
	assert.Equal(t, newHandler.Closed.Load(), false)
}

// TestClusterUpdateOldClusterStopsAfterNewSyncs verifies that the old Cluster
// (with its kube.Client) is stopped only after the new cluster syncs.
func TestClusterUpdateOldClusterStopsAfterNewSyncs(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)

	client := kube.NewFakeClient()
	secrets := clienttest.NewWriter[*v1.Secret](t, client)

	c := NewController(client, secretNamespace, "config", meshwatcher.NewTestWatcher(nil))
	c.ClientBuilder = TestingBuildClientsFromConfig

	// Track clusters using atomic pointers to avoid data races
	var oldCluster, newCluster atomic.Pointer[Cluster]

	component := BuildMultiClusterComponent(c, func(cluster *Cluster) testHandler {
		if cluster.ID == "c0" {
			if oldCluster.Load() == nil {
				oldCluster.Store(cluster)
			} else {
				newCluster.Store(cluster)
			}
		}
		return testHandler{
			ID:     cluster.ID,
			Closed: uberatomic.NewBool(false),
			Synced: uberatomic.NewBool(true),
		}
	})
	_ = component

	client.RunAndWait(stop)
	assert.NoError(t, c.Run(stop))
	retry.UntilOrFail(t, c.HasSynced, retry.Timeout(2*time.Second))

	// Create initial cluster
	secret0 := makeSecret(secretNamespace, "s0", clusterCredential{"c0", []byte("kubeconfig-initial")})
	secrets.Create(secret0)

	// Wait for cluster to be created
	retry.UntilOrFail(t, func() bool {
		return oldCluster.Load() != nil
	}, retry.Timeout(2*time.Second))

	// Update the secret
	secret0Updated := makeSecret(secretNamespace, "s0", clusterCredential{"c0", []byte("kubeconfig-rotated")})
	secrets.Update(secret0Updated)

	// Wait for new cluster to be created
	retry.UntilOrFail(t, func() bool {
		return newCluster.Load() != nil
	}, retry.Timeout(2*time.Second))

	oldC := oldCluster.Load()
	newC := newCluster.Load()

	// Verify old and new clusters are different instances
	assert.Equal(t, oldC != newC, true)

	// Wait for old cluster to be stopped (should happen after new cluster syncs via PendingClusterSwap.Complete())
	retry.UntilOrFail(t, func() bool {
		return oldC.Closed()
	}, retry.Timeout(2*time.Second))

	// Verify new cluster is NOT stopped
	assert.Equal(t, newC.Closed(), false)
}
