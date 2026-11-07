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

package ambient

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestUnreadableRemoteMeshDoesNotBlockInitialLocalFallback(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	test.SetForTest(t, &features.EnableMCSHost, false)
	client := kube.NewFakeClient()
	t.Cleanup(client.Shutdown)
	stop := test.NewStop(t)
	watcher := meshwatcher.NewTestWatcher(nil)
	local := protomarshal.Clone(watcher.Mesh())
	local.TrustDomain = "local.example"
	watcher.Set(local)
	var denied atomic.Int32
	builder := testingBuildClientsFromConfig(t)
	mc := multicluster.NewController(multicluster.ControllerOptions{
		Client: client, ClusterID: testC, SystemNamespace: systemNS, MeshConfig: watcher,
		ClientBuilder: func(config []byte, id cluster.ID, overrides ...func(*rest.Config)) (kube.Client, error) {
			remote, err := builder(config, id, overrides...)
			if err != nil {
				return nil, err
			}
			remote.Kube().(*kfake.Clientset).PrependReactor("list", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
				if a.GetNamespace() != systemNS {
					return false, nil, nil
				}
				denied.Add(1)
				return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "istio", fmt.Errorf("remote mesh read denied"))
			})
			return remote, nil
		},
	})
	assert.NoError(t, mc.Run(stop))
	client.RunAndWait(stop)
	_, err := client.Kube().CoreV1().Secrets(secretNamespace).Create(context.Background(),
		makeSecret(secretNamespace, "preexisting-denied", clusterCredential{"preexisting-denied-cluster", []byte("fake-config")}), metav1.CreateOptions{})
	assert.NoError(t, err)
	assert.EventuallyEqual(t, mc.HasSynced, true)

	// Discover the unreadable remote before releasing the local initial state.
	localSource := krt.NewStatic(&meshwatcher.MeshConfigResource{MeshConfig: local}, false, krt.WithStop(stop))
	configs := buildGlobalTrustDomainCollections(mc, meshwatcher.ConfigAdapter(localSource),
		Options{ClusterID: testC, SystemNamespace: systemNS}, krt.NewOptionsBuilder(stop, "", nil))
	assert.EventuallyEqual(t, func() bool { return denied.Load() > 0 }, true)
	assert.Equal(t, []cluster.ID{"preexisting-denied-cluster"}, mc.ListClusters())
	localSource.MarkSynced()
	assert.EventuallyEqual(t, configs.ClusterTrustDomains.HasSynced, true)
	resolved := krt.NewSingleton(func(ctx krt.HandlerContext) *string {
		td := configs.FetchTrustDomain(ctx, "preexisting-denied-cluster")
		return &td
	}, krt.WithStop(stop))
	check := func(want string) {
		t.Helper()
		retry.UntilSuccessOrFail(t, func() error {
			v := resolved.Get()
			if v == nil || *v != want {
				return fmt.Errorf("unreadable remote blocks local fallback %s; merged synced=%v", want, configs.ClusterTrustDomains.HasSynced())
			}
			return nil
		}, retry.Timeout(2*time.Second))
	}
	check("local.example")
	updated := protomarshal.Clone(local)
	updated.TrustDomain = "local-updated.example"
	localSource.Set(&meshwatcher.MeshConfigResource{MeshConfig: updated})
	check("local-updated.example")
}
