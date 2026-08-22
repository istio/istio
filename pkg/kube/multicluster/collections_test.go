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
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	return kube.NewFakeClient(
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "ns", Labels: map[string]string{"kubernetes.io/metadata.name": "ns"}},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		},
	)
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
	var localCollection krt.Collection[*v1.Service] = local
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
