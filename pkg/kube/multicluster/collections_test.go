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

	// remote returns the key (the collection's uid) and contents of the single remote cluster's
	// collection, so we can tell a rebuilt collection from a reused one.
	localKey := krt.GetKey(localCollection)
	type remoteState struct {
		key      string
		services sets.String
	}
	remote := func() remoteState {
		for _, col := range nested.List() {
			if krt.GetKey(col) == localKey {
				continue
			}
			return remoteState{
				key: krt.GetKey(col),
				services: sets.New(slices.Map(col.List(), func(s *v1.Service) string {
					return s.Name
				})...),
			}
		}
		return remoteState{}
	}

	c.AddSecret("s0", "c0")
	c.Run(stop)
	retry.UntilOrFail(t, c.controller.HasSynced, retry.Timeout(2*time.Second))
	assert.EventuallyEqual(t, func() sets.String { return remote().services }, sets.New("initial"))
	before := remote()

	// Rotate the credentials: same cluster ID, new kubeconfig.
	c.AddSecret("s0", "c0")

	// The cluster's collection must now be served from the new client...
	assert.EventuallyEqual(t, func() sets.String { return remote().services }, sets.New("later"))
	// ...by a newly built collection, and the previous one must be dropped rather than accumulated.
	after := remote()
	if after.key == before.key {
		t.Fatalf("expected the cluster's collection to be rebuilt on update, but it is still %v", after.key)
	}
	assert.Equal(t, len(nested.List()), 2, "expected only the local and the current remote collection")
}
