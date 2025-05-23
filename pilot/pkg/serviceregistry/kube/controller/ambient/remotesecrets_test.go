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

package ambient

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/ptr"
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

type simpleCluster struct {
	ID           cluster.ID
	SourceSecret types.NamespacedName
	InitialSync  bool
}

func makeSecret(namespace string, secret string, clusterConfigs ...clusterCredential) *corev1.Secret {
	s := &corev1.Secret{
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

func TestBuildRemoteClustersCollection(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "remote-cluster-secret",
			Namespace: "istio-system",
			Labels: map[string]string{
				MultiClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{
			"my-cluster": []byte("irrelevant"),
		},
	}
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "istio-system",
			Labels: map[string]string{
				label.TopologyNetwork.Name: "network-1",
			},
		},
	}
	tests := []struct {
		name            string
		options         Options
		filter          kclient.Filter
		configOverrides []func(*rest.Config)
		expectedError   bool
	}{
		{
			name: "successful build",
			options: Options{
				Client:          kube.NewFakeClient(secret),
				ClusterID:       "local-cluster",
				SystemNamespace: "istio-system",
			},
			filter: kclient.Filter{
				Namespace: "istio-system",
			},
			expectedError: false,
		},
		{
			name: "in-cluster config error",
			options: Options{
				Client:          kube.NewFakeClient(secret),
				ClusterID:       "local-cluster",
				SystemNamespace: "istio-system",
			},
			filter: kclient.Filter{
				Namespace: "istio-system",
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(tt.options.Client.Shutdown)
			opts := krt.NewOptionsBuilder(test.NewStop(t), "test", krt.GlobalDebugHandler)
			builderClient := kube.NewFakeClient(namespace)
			builder := func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
				if tt.expectedError {
					return nil, errors.New("fake err")
				}

				return builderClient, nil
			}
			tt.options.ClientBuilder = builder
			a := newAmbientTestServerFromOptions(t, testNW, tt.options, true)

			assert.Equal(t, a.remoteClusters.WaitUntilSynced(opts.Stop()), true)

			listClusters := func() int {
				return len(a.remoteClusters.List())
			}

			if tt.expectedError {
				assert.Equal(t, listClusters(), 0)
			} else {
				assert.EventuallyEqual(t, listClusters, 1)
			}
		})
	}
}

func TestKubeConfigOverride(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	var (
		expectedQPS   = float32(100)
		expectedBurst = 200
	)
	fakeRestConfig := &rest.Config{}
	client := kube.NewFakeClient()
	stopCh := test.NewStop(t)
	opts := krt.NewOptionsBuilder(test.NewStop(t), "test", krt.GlobalDebugHandler)
	builder := func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		for _, override := range configOverrides {
			override(fakeRestConfig)
		}
		return kube.NewFakeClient(), nil
	}
	options := Options{
		Client:        client,
		ClusterID:     "local-cluster",
		ClientBuilder: builder,
		RemoteClientConfigOverrides: []func(*rest.Config){
			func(cfg *rest.Config) {
				cfg.QPS = expectedQPS
				cfg.Burst = expectedBurst
			},
		},
	}
	t.Cleanup(options.Client.Shutdown)
	a := newAmbientTestServerFromOptions(t, testNW, options, false)
	a.clientBuilder = builder
	clusters := a.remoteClusters

	client.RunAndWait(stopCh)
	assert.Equal(t, clusters.WaitUntilSynced(opts.Stop()), true)
	secret0 := makeSecret(secretNamespace, "s0", clusterCredential{"c0", []byte("kubeconfig0-0")})
	secrets := a.sec
	t.Run("test kube config override", func(t *testing.T) {
		secrets.Create(secret0)
		assert.EventuallyEqual(t, func() bool {
			return clusters.GetKey("c0") != nil
		}, true)
		assert.Equal(t, fakeRestConfig, &rest.Config{
			QPS:   expectedQPS,
			Burst: expectedBurst,
		})
	})
}

func testingBuildClientsFromConfig(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
	return kube.NewFakeClient(), nil
}

type testController struct {
	clusters krt.Collection[*multicluster.Cluster]

	client  kube.Client
	t       *testing.T
	secrets clienttest.TestWriter[*corev1.Secret]
	mesh    meshwatcher.WatcherCollection
}

func buildTestControllerBase(t *testing.T, startClient bool) testController {
	tc := testController{
		client: kube.NewFakeClient(),
		t:      t,
	}
	watcher := meshwatcher.NewTestWatcher(nil)
	options := Options{
		Client:          tc.client,
		ClusterID:       "local-cluster",
		SystemNamespace: "istio-system",
		DomainSuffix:    "company.com",
		MeshConfig:      watcher,
		ClientBuilder:   testingBuildClientsFromConfig,
	}
	t.Cleanup(options.Client.Shutdown)
	a := newAmbientTestServerFromOptions(t, testNW, options, startClient)
	tc.clusters = a.remoteClusters
	tc.mesh = watcher
	tc.secrets = a.sec
	return tc
}

func buildTestController(t *testing.T) testController {
	return buildTestControllerBase(t, true)
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

func TestListRemoteClusters(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	tc := buildTestController(t)
	tc.AddSecret("s0", "c0")
	tc.AddSecret("s1", "c1")
	// before sync
	getSimpleClusters := func() []simpleCluster {
		clusters := tc.clusters.List()
		sortedClusters := slices.SortBy(clusters, func(c *multicluster.Cluster) cluster.ID {
			return c.ID
		})
		return slices.Map(sortedClusters, func(c *multicluster.Cluster) simpleCluster {
			sc := simpleCluster{
				ID:           c.ID,
				InitialSync:  c.HasSynced() && !c.SyncDidTimeout(),
				SourceSecret: c.SourceSecret,
			}
			return sc
		})
	}

	assert.EventuallyEqual(t, getSimpleClusters, []simpleCluster{
		{ID: "c0", SourceSecret: types.NamespacedName{Name: "s0", Namespace: "istio-system"}, InitialSync: true},
		{ID: "c1", SourceSecret: types.NamespacedName{Name: "s1", Namespace: "istio-system"}, InitialSync: true},
	})
}

func TestShutdown(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	stop := make(chan struct{}) // Don't use test stop because we manually close it
	tc := buildTestControllerBase(t, false)
	tc.client.RunAndWait(stop)
	tc.AddSecret("s0", "c0")
	tc.AddSecret("s1", "c1")
	retry.UntilOrFail(t, tc.clusters.HasSynced, retry.Timeout(2*time.Second))

	// Remove secret, it should be marked as closed
	var c *multicluster.Cluster
	assert.EventuallyEqual(t, func() bool {
		c = ptr.Flatten(tc.clusters.GetKey("c0"))
		return c != nil
	}, true)
	tc.DeleteSecret("s0")
	fetchClosed := func() bool {
		return c.Closed()
	}
	clustersLen := func() int {
		return len(tc.clusters.List())
	}
	assert.EventuallyEqual(t, fetchClosed, true)
	// We should still have 1 cluster left in the store
	assert.EventuallyEqual(t, clustersLen, 1)

	// close everything
	close(stop)

	// We should *not* shutdown anything else except the config cluster
	// In theory we could, but we only shut down the controller when the entire application is closing so we don't bother
	assert.EventuallyEqual(t, clustersLen, 1)
}

// TestObjectFilter tests that when a component is created, it should have access to the objectfilter.
// This ensures we do not load everything, then later filter it out.
func TestObjectFilter(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	stop := test.NewStop(t)
	clientWithNamespace := func() kube.Client {
		return kube.NewFakeClient(
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "allowed",
					Labels: map[string]string{"kubernetes.io/metadata.name": "allowed"},
				},
			},
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "not-allowed",
					Labels: map[string]string{"kubernetes.io/metadata.name": "not-allowed"},
				},
			},
		)
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

	tc := testController{
		client: clientWithNamespace(),
		t:      t,
	}

	clusterID := cluster.ID("config")
	// For primary cluster, we need to set it up ourselves.
	namespaces := kclient.New[*corev1.Namespace](tc.client)
	filter := namespace.NewDiscoveryNamespacesFilter(namespaces, mesh, stop)
	tc.client = kube.SetObjectFilter(tc.client, filter)
	tc.secrets = clienttest.NewWriter[*corev1.Secret](t, tc.client)
	options := Options{
		Client:          tc.client,
		ClusterID:       clusterID,
		SystemNamespace: "istio-system",
		DomainSuffix:    "company.com",
		MeshConfig:      mesh,
		ClientBuilder: func(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
			return clientWithNamespace(), nil
		},
	}
	a := newAmbientTestServerFromOptions(t, testNW, options, true)

	tc.clusters = a.remoteClusters
	tc.mesh = mesh

	var wg sync.WaitGroup
	_ = krt.NewCollection(tc.clusters, func(ctx krt.HandlerContext, cluster *multicluster.Cluster) *testHandler {
		assert.Equal(t, cluster.Client.ObjectFilter() != nil, true, "cluster "+cluster.ID.String())
		assert.Equal(t, cluster.Client.ObjectFilter().Filter("allowed"), true)
		assert.Equal(t, cluster.Client.ObjectFilter().Filter("not-allowed"), false)
		wg.Done()
		return &testHandler{
			ID:     cluster.ID,
			Closed: atomic.NewBool(false),
			Synced: atomic.NewBool(true),
		}
	})

	tc.AddSecret("s0", "c0")
	tc.AddSecret("s1", "c1")
	wg.Add(2) // 1 per cluster
	retry.UntilOrFail(t, func() bool {
		s := tc.clusters.HasSynced()
		return s
	}, retry.Timeout(2*time.Second))
	// Test the local cluster since the collection is only run for remote clusters
	assert.Equal(t, tc.client.ObjectFilter() != nil, true, "cluster "+clusterID.String())
	assert.Equal(t, tc.client.ObjectFilter().Filter("allowed"), true)
	assert.Equal(t, tc.client.ObjectFilter().Filter("not-allowed"), false)
	wg.Wait() // Make sure we evaluate the inside of the collection
}

type informerHandler[T controllers.ComparableObject] struct {
	client    kclient.Client[T]
	clusterID cluster.ID
}

func (i *informerHandler[T]) Close() {
	i.client.ShutdownHandlers()
}

func (i *informerHandler[T]) HasSynced() bool {
	return i.client.HasSynced()
}

func (i *informerHandler[T]) ResourceName() string {
	return i.clusterID.String()
}

// Test our (lack of) ability to do seamless updates of a cluster.
// Tracking improvements in https://github.com/istio/istio/issues/49349
func TestSeamlessMigration(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)

	tt := assert.NewTracker[string](t)
	initial := kube.NewFakeClient(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "initial"},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "common"},
		},
	)
	later := kube.NewFakeClient(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "later"},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "common"},
		},
	)

	tc := testController{
		client: kube.NewFakeClient(),
		t:      t,
	}
	watcher := meshwatcher.NewTestWatcher(nil)
	nextClient := initial

	options := Options{
		Client:          tc.client,
		ClusterID:       "local-cluster",
		SystemNamespace: "istio-system",
		DomainSuffix:    "company.com",
		MeshConfig:      watcher,
		ClientBuilder: func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
			ret := nextClient
			nextClient = later
			return ret, nil
		},
	}
	t.Cleanup(options.Client.Shutdown)

	a := newAmbientTestServerFromOptions(t, testNW, options, true)
	tc.clusters = a.remoteClusters
	tc.mesh = watcher
	tc.secrets = a.sec

	infs := krt.NewCollection(tc.clusters, func(ctx krt.HandlerContext, cluster *multicluster.Cluster) **informerHandler[*corev1.ConfigMap] {
		cl := kclient.New[*corev1.ConfigMap](cluster.Client)
		cl.AddEventHandler(clienttest.TrackerHandler(tt))
		return ptr.Of(&informerHandler[*corev1.ConfigMap]{client: cl, clusterID: cluster.ID})
	})
	tc.AddSecret("s0", "c0")

	retry.UntilOrFail(t, tc.clusters.HasSynced, retry.Timeout(2*time.Second))
	retry.UntilOrFail(t, infs.HasSynced, retry.Timeout(2*time.Second))
	var c0Client *informerHandler[*corev1.ConfigMap]
	assert.EventuallyEqual(t, func() bool {
		c0Client = ptr.Flatten(infs.GetKey("c0"))
		return c0Client != nil
	}, true)
	retry.UntilOrFail(t, c0Client.HasSynced, retry.Timeout(2*time.Second))
	assert.Equal(t,
		clienttest.Names(c0Client.client.List(metav1.NamespaceAll, klabels.Everything())),
		sets.New("initial", "common"))

	tt.WaitUnordered("add/common", "add/initial")

	// Update the cluster
	tc.AddSecret("s0", "c0")
	var fatal error
	retry.UntilOrFail(t, func() bool {
		var c0Client *informerHandler[*corev1.ConfigMap]
		assert.EventuallyEqual(t, func() bool {
			c0Client = ptr.Flatten(infs.GetKey("c0"))
			return c0Client != nil
		}, true)
		have := clienttest.Names(c0Client.client.List(metav1.NamespaceAll, klabels.Everything()))
		if have.Equals(sets.New("later", "common")) {
			return true
		}
		if !have.Equals(sets.New("initial", "common")) {
			fatal = fmt.Errorf("unexpected contents: %v", have)
			// TODO: return true here, then assert.NoError(t, fatal) after
			// This would properly check that we do not go from `old -> empty -> new` and instead go from `old -> new` seamlessly
			// However, the code does not currently handle this case.
			return false
		}
		return false
	}, retry.Timeout(5*time.Second))
	_ = fatal
	// We get ADD again! Oops. Ideally we would be abstracted from the cluster update and instead get 'delete/initial, add/later, update/common'.
	// See discussion in https://github.com/istio/enhancements/pull/107
	tt.WaitUnordered("add/common", "add/later")
}

func TestSecretController(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
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
		add    *corev1.Secret
		update *corev1.Secret
		delete *corev1.Secret

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

	stopCh := test.NewStop(t)
	watcher := meshwatcher.NewTestWatcher(nil)
	tc := testController{
		client: client,
		t:      t,
	}

	options := Options{
		Client:          tc.client,
		ClusterID:       "config",
		SystemNamespace: "istio-system",
		DomainSuffix:    "company.com",
		MeshConfig:      watcher,
	}
	t.Cleanup(options.Client.Shutdown)

	a := newAmbientTestServerFromOptions(t, testNW, options, false)
	tc.clusters = a.remoteClusters
	tc.mesh = watcher
	tc.secrets = a.sec
	secrets := tc.secrets
	iter := 1 // the config cluster starts at 1, so we'll start at 2

	remoteHandlers := krt.NewCollection(tc.clusters, func(ctx krt.HandlerContext, cluster *multicluster.Cluster) *testHandler {
		iter++
		return &testHandler{
			ID:     cluster.ID,
			Iter:   iter,
			Closed: atomic.NewBool(false),
			Synced: atomic.NewBool(false),
		}
	})
	handlers := krt.JoinCollection([]krt.Collection[testHandler]{
		krt.NewSingleton(func(ctx krt.HandlerContext) *testHandler {
			return &testHandler{
				ID:     cluster.ID("config"),
				Iter:   1,
				Closed: atomic.NewBool(false),
				Synced: atomic.NewBool(false),
			}
		}).AsCollection(),
		remoteHandlers,
	})
	client.RunAndWait(stopCh)
	// TODO: Can't delay sync of the local cluster in tests right now; would be good to add it sometime
	t.Run("sync timeout", func(t *testing.T) {
		retry.UntilOrFail(t, tc.clusters.HasSynced, retry.Timeout(2*time.Second))
	})
	kube.WaitForCacheSync("test", stopCh, tc.clusters.HasSynced, handlers.HasSynced)

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
				res := slices.Map(handlers.List(), func(e testHandler) result {
					return result{e.ID, e.Iter}
				})
				return res
			}, step.want)
		})
	}
}

type testHandler struct {
	ID     cluster.ID
	Iter   int
	Closed *atomic.Bool
	Synced *atomic.Bool
}

func (h testHandler) Close() {
	h.Closed.Store(true)
}

func (h testHandler) HasSynced() bool {
	return h.Synced.Load()
}

func (h testHandler) ResourceName() string {
	return h.ID.String()
}
