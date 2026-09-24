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

package multicluster

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"go.uber.org/atomic"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

func fakeClientWithService(name string) kube.Client {
	return fakeClientWithServices(name)
}

func fakeClientWithServices(names ...string) kube.Client {
	objects := []runtime.Object{
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "ns", Labels: map[string]string{"kubernetes.io/metadata.name": "ns"}},
		},
	}
	for _, name := range names {
		objects = append(objects, &v1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		})
	}
	return kube.NewFakeClient(objects...)
}

// TestNestedCollectionsRebuiltOnClusterUpdate verifies that updating a cluster's credentials (a secret
// update carrying a new kubeconfig for the same cluster ID) replaces that cluster's nested collections
// with new ones built on the new client.
//
// The previous generation's client and informers are shut down once the new generation syncs, so
// continuing to serve its collections would freeze the cluster's contents at rotation time.
func TestNestedCollectionsRebuiltOnClusterUpdate(t *testing.T) {
	stop := test.NewStop(t)
	c := buildTestController(t, true)

	// Hand out a different client for each generation of the cluster, with distinguishable contents.
	nextClient := fakeClientWithService("initial")
	c.controller.ClientBuilder = func(kubeConfig []byte, clusterID cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		ret := nextClient
		nextClient = fakeClientWithService("later")
		return ret, nil
	}

	opts := krt.NewOptionsBuilder(stop, "test", krt.GlobalDebugHandler)
	local := krt.NewStaticCollection[*v1.Service](nil, nil, opts.WithName("LocalServices")...)
	localCollection := local
	nested := NestedCollectionFromLocalAndRemote(
		c.controller,
		localCollection,
		func(ctx krt.HandlerContext, cl *Cluster) *krt.Collection[*v1.Service] {
			if !cl.hasInitialCollections() {
				return nil
			}
			return ptr.Of(cl.Services())
		},
		"Services",
		opts,
	)

	// remoteServices returns the contents of the remote cluster's collection. A collection is keyed by
	// its name, which is stable across generations, so the contents are what tell a rebuilt collection
	// from a reused one: the two generations' clients hold different Services.
	localKey := krt.GetKey(localCollection)
	remoteServices := func() sets.String {
		for _, col := range nested.List() {
			if krt.GetKey(col) == localKey {
				continue
			}
			return sets.New(slices.Map(col.List(), func(s *v1.Service) string {
				return s.Name
			})...)
		}
		return nil
	}
	// The cluster contributes exactly one collection at all times, including across the swap. Consumers
	// index this collection by cluster and read it with krt.FetchOne, which tolerates neither zero nor
	// two.
	assertSingleRemote := func() {
		t.Helper()
		if got := len(nested.List()); got != 2 {
			t.Fatalf("expected the local collection and exactly one remote collection, got %d", got)
		}
	}

	c.AddSecret("s0", "c0")
	c.Run(stop)
	retry.UntilOrFail(t, c.controller.HasSynced, retry.Timeout(2*time.Second))
	assert.EventuallyEqual(t, remoteServices, sets.New("initial"))
	assertSingleRemote()

	// Rotate the credentials: same cluster ID, new kubeconfig. The collection must be rebuilt against
	// the new client, which is observable as its contents changing.
	c.AddSecret("s0", "c0")
	assert.EventuallyEqual(t, remoteServices, sets.New("later"))
	assertSingleRemote()
}

// rotationScenario drives a cluster through two credential rotations and reports what the collections
// derived from the nested collection saw. buildNested is the entry point under test, and mirrors how
// callers turn a cluster into the collections it contributes.
type rotationScenario struct {
	// buildNested builds the collection of collections under test.
	buildNested func(ctrl *Controller, local krt.Collection[*v1.Service], opts krt.OptionsBuilder) krt.Collection[krt.Collection[*v1.Service]]
	// collectionsPerCluster is how many collections buildNested makes each cluster contribute.
	collectionsPerCluster int
	// initialItems are the merged items expected once the cluster is first added, and postRotationItems
	// the ones expected after a rotation that adds a Service to the cluster.
	initialItems      []string
	postRotationItems []string
}

// runRotationScenario checks that rotating a cluster's credentials does not churn the collections
// derived from the nested collection: replacing a cluster's collections with equivalent ones built on
// the new client is not an addition or a removal of anything, so nothing downstream may be told that
// its items came or went.
func runRotationScenario(t *testing.T, s rotationScenario) {
	t.Helper()

	stop := test.NewStop(t)
	c := buildTestController(t, true)

	// Every generation of the cluster gets its own client. The first two hold the same Services, so a
	// rotation between them changes nothing; the third adds one, which is a real addition.
	generations := [][]string{{"svc"}, {"svc"}, {"svc", "later"}}
	built := atomic.NewInt32(0)
	c.controller.ClientBuilder = func(kubeConfig []byte, clusterID cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
		generation := int(built.Inc()) - 1
		if generation >= len(generations) {
			generation = len(generations) - 1
		}
		return fakeClientWithServices(generations[generation]...), nil
	}

	opts := krt.NewOptionsBuilder(stop, "test", krt.GlobalDebugHandler)
	local := krt.NewStaticCollection[*v1.Service](nil, nil, opts.WithName("LocalServices")...)
	nested := s.buildNested(c.controller, local, opts)

	// A merged view of everything the clusters hold, which is what consumers of these nested collections
	// actually read.
	merged := krt.NestedJoinWithMergeCollection(nested, func(ts []*v1.Service) **v1.Service {
		if len(ts) == 0 {
			return nil
		}
		return &ts[0]
	}, opts.With(krt.WithName("MergedServices"))...)

	events := assert.NewTracker[string](t)
	merged.RegisterBatch(func(batch []krt.Event[*v1.Service]) {
		for _, e := range batch {
			events.Record(fmt.Sprintf("%v/%v", e.Event, krt.GetKey(e.Latest())))
		}
	}, true)

	// The cluster contributes a fixed number of collections at all times, including across a swap.
	// Consumers index this collection by cluster and read it with krt.FetchOne, which tolerates neither
	// zero nor two.
	assertCollectionCount := func() {
		t.Helper()
		want := 1 + s.collectionsPerCluster
		if got := len(nested.List()); got != want {
			t.Fatalf("expected the local collection and %d for the cluster, got %d", s.collectionsPerCluster, got)
		}
	}
	// waitForRotation waits until the cluster is being served by a generation other than the one it was
	// on. Rotations have to be applied one at a time: writing the secret twice in a row can be collapsed
	// into a single event by the informer, and starting a rotation while the previous one is still
	// syncing is a different scenario from the one under test here.
	var serving [sha256.Size]byte
	waitForRotation := func() {
		t.Helper()
		previous := serving
		retry.UntilOrFail(t, func() bool {
			cl := c.controller.cs.GetByID("c0")
			if cl == nil || cl.kubeConfigSha == previous || !clusterReady(cl) {
				return false
			}
			serving = cl.kubeConfigSha
			return true
		}, retry.Timeout(10*time.Second))
	}

	c.AddSecret("s0", "c0")
	c.Run(stop)
	retry.UntilOrFail(t, c.controller.HasSynced, retry.Timeout(10*time.Second))
	waitForRotation()
	events.WaitUnordered(s.initialItems...)
	assertCollectionCount()

	// Rotate the credentials: same cluster ID, same contents, new kubeconfig. Nothing downstream changed,
	// so nothing may be reported. "No event" cannot be asserted by looking at an empty tracker right
	// after the rotation, so the next rotation, which does add a Service, doubles as the proof: any
	// spurious event from this one would be reported before that addition.
	c.AddSecret("s0", "c0")
	waitForRotation()
	assertCollectionCount()

	// Rotate once more, this time onto a cluster holding an extra Service. Only that Service may be
	// reported, and only as an addition: the ones the cluster already held are still there.
	c.AddSecret("s0", "c0")
	waitForRotation()
	events.WaitUnordered(s.postRotationItems...)
	assertCollectionCount()
}

// TestNestedCollectionFromLocalAndRemoteRotation checks that a rotation does not churn the collections
// derived from a NestedCollectionFromLocalAndRemote.
func TestNestedCollectionFromLocalAndRemoteRotation(t *testing.T) {
	runRotationScenario(t, rotationScenario{
		buildNested: func(ctrl *Controller, local krt.Collection[*v1.Service], opts krt.OptionsBuilder) krt.Collection[krt.Collection[*v1.Service]] {
			return NestedCollectionFromLocalAndRemote(
				ctrl,
				local,
				func(ctx krt.HandlerContext, cl *Cluster) *krt.Collection[*v1.Service] {
					if !cl.hasInitialCollections() {
						return nil
					}
					return ptr.Of(cl.Services())
				},
				"Services",
				opts,
			)
		},
		collectionsPerCluster: 1,
		initialItems:          []string{"add/ns/svc"},
		postRotationItems:     []string{"add/ns/later"},
	})
}

// TestNestedManyCollectionsFromLocalAndRemoteRotation checks the same for a cluster that contributes
// more than one collection, including one built by the transformation itself rather than taken from
// the cluster.
func TestNestedManyCollectionsFromLocalAndRemoteRotation(t *testing.T) {
	runRotationScenario(t, rotationScenario{
		buildNested: func(ctrl *Controller, local krt.Collection[*v1.Service], opts krt.OptionsBuilder) krt.Collection[krt.Collection[*v1.Service]] {
			return NestedManyCollectionsFromLocalAndRemote(
				ctrl,
				[]krt.Collection[*v1.Service]{local},
				func(ctx krt.HandlerContext, cl *Cluster) []krt.Collection[*v1.Service] {
					if !cl.hasInitialCollections() {
						return nil
					}
					// The mirror is named after the cluster and stopped with it, the way per-cluster
					// collections are built in the ambient index.
					// NewCollection rather than MapCollection: the mirror renames what it holds, and a
					// mapped collection must keep the keys of the one it maps.
					mirror := krt.NewCollection(cl.Services(), func(ctx krt.HandlerContext, svc *v1.Service) **v1.Service {
						out := svc.DeepCopy()
						out.Name = "mirror-" + svc.Name
						return &out
					},
						krt.WithName(fmt.Sprintf("MirrorServices[%s]", cl.ID)),
						krt.WithStop(cl.GetStop()),
						krt.WithDebugging(opts.Debugger()),
					)
					return []krt.Collection[*v1.Service]{cl.Services(), mirror}
				},
				"Services",
				opts,
			)
		},
		collectionsPerCluster: 2,
		initialItems:          []string{"add/ns/svc", "add/ns/mirror-svc"},
		postRotationItems:     []string{"add/ns/later", "add/ns/mirror-later"},
	})
}
