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
	"testing"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
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
			opts := krt.NewOptionsBuilder(test.NewStop(t), "test", krt.GlobalDebugHandler)
			builderClient := kube.NewFakeClient(namespace)
			builder := func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
				if tt.expectedError {
					return nil, errors.New("fake err")
				}

				return builderClient, nil
			}
			a := newAmbientTestServer(t, testC, testNW)
			a.clientBuilder = builder
			clusters := a.remoteClusters
			tt.options.Client.RunAndWait(opts.Stop()) // Wait for the client in options to be ready
			assert.Equal(t, clusters.WaitUntilSynced(opts.Stop()), true)

			if tt.expectedError {
				assert.Equal(t, len(clusters.List()), 0)
			} else {
				assert.Equal(t, len(clusters.List()), 1)
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
	a := newAmbientTestServer(t, testC, testNW)
	a.clientBuilder = builder
	clusters := a.remoteClusters

	client.RunAndWait(stopCh)
	assert.Equal(t, clusters.WaitUntilSynced(opts.Stop()), true)
	secret0 := makeSecret(secretNamespace, "s0", clusterCredential{"c0", []byte("kubeconfig0-0")})
	secrets := clienttest.NewWriter[*corev1.Secret](t, client)
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

func TestingBuildClientsFromConfig(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
	return kube.NewFakeClient(), nil
}

type testController struct {
	clusters krt.Collection[Cluster]

	client  kube.Client
	t       *testing.T
	secrets clienttest.TestWriter[*corev1.Secret]
	mesh    meshwatcher.WatcherCollection
}

func buildTestController(t *testing.T) testController {
	tc := testController{
		client: kube.NewFakeClient(),
		t:      t,
	}
	tc.secrets = clienttest.NewWriter[*corev1.Secret](t, tc.client)
	watcher := meshwatcher.NewTestWatcher(nil)
	options := Options{
		Client:          tc.client,
		ClusterID:       "local-cluster",
		SystemNamespace: "istio-system",
		DomainSuffix:    "company.com",
		MeshConfig:      watcher,
	}
	a := newAmbientTestServerFromOptions(t, testNW, options)
	a.clientBuilder = TestingBuildClientsFromConfig
	tc.clusters = a.remoteClusters
	tc.mesh = watcher
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

func (t *testController) Run(stop chan struct{}) {
	t.client.RunAndWait(stop)
}

func TestListRemoteClusters(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	stop := make(chan struct{})
	tc := buildTestController(t)
	tc.AddSecret("s0", "c0")
	tc.AddSecret("s1", "c1")
	tc.Run(stop)

	// before sync
	getSimpleClusters := func() []simpleCluster {
		clusters := tc.clusters.List()
		sortedClusters := slices.SortBy(clusters, func(c Cluster) cluster.ID {
			return c.ID
		})
		return slices.Map(sortedClusters, func(c Cluster) simpleCluster {
			sc := simpleCluster{
				ID:           c.ID,
				InitialSync:  c.initialSync.Load(),
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
	tc := buildTestController(t)
	tc.AddSecret("s0", "c0")
	tc.AddSecret("s1", "c1")
	tc.Run(stop)
	retry.UntilOrFail(t, tc.clusters.HasSynced, retry.Timeout(2*time.Second))

	// Remove secret, it should be marked as closed
	var c *Cluster
	assert.EventuallyEqual(t, func() bool {
		c = tc.clusters.GetKey("c0")
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

	// For primary cluster, we need to set it up ourselves.
	namespaces := kclient.New[*corev1.Namespace](tc.client)
	filter := namespace.NewDiscoveryNamespacesFilter(namespaces, mesh, stop)
	tc.client = kube.SetObjectFilter(tc.client, filter)
	tc.secrets = clienttest.NewWriter[*corev1.Secret](t, tc.client)
	options := Options{
		Client:          tc.client,
		ClusterID:       "config",
		SystemNamespace: secretNamespace,
		DomainSuffix:    "company.com",
		MeshConfig:      mesh,
	}
	a := newAmbientTestServerFromOptions(t, testNW, options)
	a.clientBuilder = func(kubeConfig []byte, c cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		return clientWithNamespace(), nil
	}
	tc.clusters = a.remoteClusters
	tc.mesh = mesh

	_ = krt.NewCollection(tc.clusters, func(ctx krt.HandlerContext, cluster Cluster) *testHandler {
		assert.Equal(t, cluster.Client.ObjectFilter() != nil, true, "cluster "+cluster.ID.String())
		assert.Equal(t, cluster.Client.ObjectFilter().Filter("allowed"), true)
		assert.Equal(t, cluster.Client.ObjectFilter().Filter("not-allowed"), false)
		return &testHandler{
			ID:     cluster.ID,
			Closed: atomic.NewBool(false),
			Synced: atomic.NewBool(true),
		}
	})

	tc.AddSecret("s0", "c0")
	tc.AddSecret("s1", "c1")
	tc.Run(stop)
	retry.UntilOrFail(t, tc.clusters.HasSynced, retry.Timeout(2*time.Second))
}

// type informerHandler[T controllers.ComparableObject] struct {
// 	client kclient.Client[T]
// }

// func (i *informerHandler[T]) Close() {
// 	i.client.ShutdownHandlers()
// }

// func (i *informerHandler[T]) HasSynced() bool {
// 	return i.client.HasSynced()
// }

// // Test our (lack of) ability to do seamless updates of a cluster.
// // Tracking improvements in https://github.com/istio/istio/issues/49349
// func TestSeamlessMigration(t *testing.T) {
// 	stop := make(chan struct{})
// 	c := buildTestController(t, true)
// 	tt := assert.NewTracker[string](t)
// 	initial := kube.NewFakeClient(
// 		&v1.ConfigMap{
// 			ObjectMeta: metav1.ObjectMeta{Name: "initial"},
// 		},
// 		&v1.ConfigMap{
// 			ObjectMeta: metav1.ObjectMeta{Name: "common"},
// 		},
// 	)
// 	later := kube.NewFakeClient(
// 		&v1.ConfigMap{
// 			ObjectMeta: metav1.ObjectMeta{Name: "later"},
// 		},
// 		&v1.ConfigMap{
// 			ObjectMeta: metav1.ObjectMeta{Name: "common"},
// 		},
// 	)
// 	nextClient := initial
// 	c.controller.ClientBuilder = func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
// 		ret := nextClient
// 		nextClient = later
// 		return ret, nil
// 	}
// 	component := BuildMultiClusterComponent(c.controller, func(cluster *Cluster) *informerHandler[*corev1.ConfigMap] {
// 		cl := kclient.New[*corev1.ConfigMap](cluster.Client)
// 		cl.AddEventHandler(clienttest.TrackerHandler(tt))
// 		return &informerHandler[*corev1.ConfigMap]{client: cl}
// 	})
// 	c.AddSecret("s0", "c0")
// 	c.Run(stop)
// 	retry.UntilOrFail(t, c.controller.HasSynced, retry.Timeout(2*time.Second))
// 	assert.Equal(t,
// 		clienttest.Names((*component.ForCluster("c0")).client.List(metav1.NamespaceAll, klabels.Everything())),
// 		sets.New("initial", "common"))

// 	tt.WaitUnordered("add/common", "add/initial")

// 	// Update the cluster
// 	c.AddSecret("s0", "c0")
// 	var fatal error
// 	retry.UntilOrFail(t, func() bool {
// 		have := clienttest.Names((*component.ForCluster("c0")).client.List(metav1.NamespaceAll, klabels.Everything()))
// 		if have.Equals(sets.New("later", "common")) {
// 			return true
// 		}
// 		if !have.Equals(sets.New("initial", "common")) {
// 			fatal = fmt.Errorf("unexpected contents: %v", have)
// 			// TODO: return true here, then assert.NoError(t, fatal) after
// 			// This would properly check that we do not go from `old -> empty -> new` and instead go from `old -> new` seamlessly
// 			// However, the code does not currently handler this case.
// 			return false
// 		}
// 		return false
// 	})
// 	_ = fatal
// 	// We get ADD again! Oops. Ideally we would be abstracted from the cluster update and instead get 'delete/initial, add/later, update/common'.
// 	// See discussion in https://github.com/istio/enhancements/pull/107
// 	tt.WaitUnordered("add/common", "add/later")
// }

// func TestSecretController(t *testing.T) {
// 	client := kube.NewFakeClient()

// 	var (
// 		secret0 = makeSecret(secretNamespace, "s0",
// 			clusterCredential{"c0", []byte("kubeconfig0-0")})
// 		secret0UpdateKubeconfigChanged = makeSecret(secretNamespace, "s0",
// 			clusterCredential{"c0", []byte("kubeconfig0-1")})
// 		secret0UpdateKubeconfigSame = makeSecret(secretNamespace, "s0",
// 			clusterCredential{"c0", []byte("kubeconfig0-1")})
// 		secret0AddCluster = makeSecret(secretNamespace, "s0",
// 			clusterCredential{"c0", []byte("kubeconfig0-1")}, clusterCredential{"c0-1", []byte("kubeconfig0-2")})
// 		secret0DeleteCluster = secret0UpdateKubeconfigChanged // "c0-1" cluster deleted
// 		secret0ReAddCluster  = makeSecret(secretNamespace, "s0",
// 			clusterCredential{"c0", []byte("kubeconfig0-1")}, clusterCredential{"c0-1", []byte("kubeconfig0-2")})
// 		secret0ReDeleteCluster = secret0UpdateKubeconfigChanged // "c0-1" cluster re-deleted
// 		secret1                = makeSecret(secretNamespace, "s1",
// 			clusterCredential{"c1", []byte("kubeconfig1-0")})
// 		otherNSSecret = makeSecret("some-other-namespace", "s2",
// 			clusterCredential{"c1", []byte("kubeconfig1-0")})
// 		secret2Cluster0 = makeSecret(secretNamespace, "s2",
// 			clusterCredential{"c0", []byte("kubeconfig1-1")})
// 		configCluster = makeSecret(secretNamespace, "s3",
// 			clusterCredential{"config", []byte("kubeconfig3-0")})
// 	)

// 	secret0UpdateKubeconfigSame.Annotations = map[string]string{"foo": "bar"}

// 	type result struct {
// 		ID   cluster.ID
// 		Iter int
// 	}

// 	steps := []struct {
// 		name string
// 		// only set one of these per step. The others should be nil.
// 		add    *corev1.Secret
// 		update *corev1.Secret
// 		delete *corev1.Secret

// 		want []result
// 	}{
// 		{
// 			name: "Create secret s0 and add kubeconfig for cluster c0, which will add remote cluster c0",
// 			add:  secret0,
// 			want: []result{{"config", 1}, {"c0", 2}},
// 		},
// 		{
// 			name:   "Update secret s0 and update the kubeconfig of cluster c0, which will update remote cluster c0",
// 			update: secret0UpdateKubeconfigChanged,
// 			want:   []result{{"config", 1}, {"c0", 3}},
// 		},
// 		{
// 			name:   "Update secret s0 but keep the kubeconfig of cluster c0 unchanged, which will not update remote cluster c0",
// 			update: secret0UpdateKubeconfigSame,
// 			want:   []result{{"config", 1}, {"c0", 3}},
// 		},
// 		{
// 			name: "Update secret s0 and add kubeconfig for cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
// 				"which will add remote cluster c0-1 but will not update remote cluster c0",
// 			update: secret0AddCluster,
// 			want:   []result{{"config", 1}, {"c0", 3}, {"c0-1", 4}},
// 		},
// 		{
// 			name: "Update secret s0 and delete cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
// 				"which will delete remote cluster c0-1 but will not update remote cluster c0",
// 			update: secret0DeleteCluster,
// 			want:   []result{{"config", 1}, {"c0", 3}},
// 		},
// 		{
// 			name: "Update secret s0 and re-add kubeconfig for cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
// 				"which will add remote cluster c0-1 but will not update remote cluster c0",
// 			update: secret0ReAddCluster,
// 			want:   []result{{"config", 1}, {"c0", 3}, {"c0-1", 5}},
// 		},
// 		{
// 			name: "Update secret s0 and re-delete cluster c0-1 but keep the kubeconfig of cluster c0 unchanged, " +
// 				"which will delete remote cluster c0-1 but will not update remote cluster c0",
// 			update: secret0ReDeleteCluster,
// 			want:   []result{{"config", 1}, {"c0", 3}},
// 		},
// 		{
// 			name: "Create secret s1 and add kubeconfig for cluster c1, which will add remote cluster c1",
// 			add:  secret1,
// 			want: []result{{"config", 1}, {"c0", 3}, {"c1", 6}},
// 		},
// 		{
// 			name: "Add secret s2, with already existing cluster",
// 			add:  secret2Cluster0,
// 			want: []result{{"config", 1}, {"c0", 3}, {"c1", 6}},
// 		},
// 		{
// 			name:   "Delete secret s2, with already existing cluster",
// 			delete: secret2Cluster0,
// 			want:   []result{{"config", 1}, {"c0", 3}, {"c1", 6}},
// 		},
// 		{
// 			name:   "Delete secret s0, which will delete remote cluster c0",
// 			delete: secret0,
// 			want:   []result{{"config", 1}, {"c1", 6}},
// 		},
// 		{
// 			name:   "Delete secret s1, which will delete remote cluster c1",
// 			delete: secret1,
// 			want:   []result{{"config", 1}},
// 		},
// 		{
// 			name: "Add secret from another namespace",
// 			add:  otherNSSecret,
// 			want: []result{{"config", 1}},
// 		},
// 		{
// 			name: "Add secret referencing config cluster",
// 			add:  configCluster,
// 			want: []result{{"config", 1}},
// 		},
// 		{
// 			name:   "Delete secret referencing config cluster",
// 			delete: configCluster,
// 			want:   []result{{"config", 1}},
// 		},
// 	}

// 	// Start the secret controller and sleep to allow secret process to start.
// 	stopCh := test.NewStop(t)
// 	c := NewController(client, secretNamespace, "config", meshwatcher.NewTestWatcher(nil))
// 	c.ClientBuilder = TestingBuildClientsFromConfig
// 	client.RunAndWait(stopCh)
// 	secrets := clienttest.NewWriter[*corev1.Secret](t, client)
// 	iter := 0
// 	component := BuildMultiClusterComponent(c, func(cluster *Cluster) testHandler {
// 		iter++
// 		return testHandler{
// 			ID:     cluster.ID,
// 			Iter:   iter,
// 			Closed: atomic.NewBool(false),
// 			Synced: atomic.NewBool(false),
// 		}
// 	})
// 	client.RunAndWait(stopCh)
// 	assert.NoError(t, c.Run(stopCh))
// 	// Should not be synced...
// 	assert.Equal(t, c.HasSynced(), false)
// 	// Now mark the config cluster as synced
// 	component.All()[0].Synced.Store(true)
// 	t.Run("sync timeout", func(t *testing.T) {
// 		retry.UntilOrFail(t, c.HasSynced, retry.Timeout(2*time.Second))
// 	})
// 	kube.WaitForCacheSync("test", stopCh, c.HasSynced)

// 	for _, step := range steps {
// 		t.Run(step.name, func(t *testing.T) {
// 			switch {
// 			case step.add != nil:
// 				secrets.Create(step.add)
// 			case step.update != nil:
// 				secrets.Update(step.update)
// 			case step.delete != nil:
// 				secrets.Delete(step.delete.Name, step.delete.Namespace)
// 			}

// 			assert.EventuallyEqual(t, func() []result {
// 				return slices.Map(component.All(), func(e testHandler) result {
// 					return result{e.ID, e.Iter}
// 				})
// 			}, step.want)
// 		})
// 	}
// }

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
