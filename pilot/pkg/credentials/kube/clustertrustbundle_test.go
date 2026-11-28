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

package kube

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/clustertrustbundle"
	"istio.io/istio/pkg/kube/clustertrustbundle/clustertrustbundletest"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func makeCTB(name, bundle string) *certificatesv1.ClusterTrustBundle {
	return &certificatesv1.ClusterTrustBundle{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       certificatesv1.ClusterTrustBundleSpec{TrustBundle: bundle},
	}
}

func TestGetClusterTrustBundle(t *testing.T) {
	ca := clustertrustbundletest.GenerateCA(t, "ca")
	client := kube.NewFakeClient(
		makeCTB("good", ca),
		makeCTB("garbage", "not a pem"),
	)
	clustertrustbundletest.SetAPIAvailable(client)
	stop := test.NewStop(t)
	sc := NewCredentialsController(client, nil, true, stop)
	client.RunAndWait(stop)
	assert.EventuallyEqual(t, sc.HasSynced, true)

	cases := []struct {
		name    string
		wantErr string
	}{
		{name: "good"},
		{name: "missing", wantErr: "clustertrustbundle missing not found"},
		{name: "garbage", wantErr: "valid PEM"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.EventuallyEqual(t, func() bool {
				_, err := sc.GetClusterTrustBundle(tt.name)
				return (err == nil) == (tt.wantErr == "") && (tt.wantErr == "" || strings.Contains(err.Error(), tt.wantErr))
			}, true)
			info, err := sc.GetClusterTrustBundle(tt.name)
			if tt.wantErr != "" {
				assert.Equal(t, info == nil, true)
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, string(info.Cert), ca)
			assert.Equal(t, len(info.Key), 0)
		})
	}
}

func TestGetClusterTrustBundleRemoteCluster(t *testing.T) {
	client := kube.NewFakeClient(makeCTB("good", clustertrustbundletest.GenerateCA(t, "ca")))
	clustertrustbundletest.SetAPIAvailable(client)
	stop := test.NewStop(t)
	sc := NewCredentialsController(client, nil, false, stop)
	client.RunAndWait(stop)
	assert.Equal(t, sc.clusterTrustBundles == nil, true)
	_, err := sc.GetClusterTrustBundle("good")
	assert.Error(t, err)
}

func TestGetClusterTrustBundleDisabled(t *testing.T) {
	test.SetForTest(t, &features.EnableGatewayAPIClusterTrustBundle, false)
	client := kube.NewFakeClient(makeCTB("good", clustertrustbundletest.GenerateCA(t, "ca")))
	clustertrustbundletest.SetAPIAvailable(client)
	stop := test.NewStop(t)
	sc := NewCredentialsController(client, nil, true, stop)
	client.RunAndWait(stop)
	assert.EventuallyEqual(t, sc.HasSynced, true)
	assert.Equal(t, sc.clusterTrustBundles == nil, true)
	_, err := sc.GetClusterTrustBundle("good")
	assert.Error(t, err)
}

func TestClusterTrustBundleAPINotServed(t *testing.T) {
	// The fake client does not advertise the API, so the controller must still sync and simply find nothing.
	client := kube.NewFakeClient(makeCTB("good", clustertrustbundletest.GenerateCA(t, "ca")))
	stop := test.NewStop(t)
	sc := NewCredentialsController(client, nil, true, stop)
	client.RunAndWait(stop)
	assert.EventuallyEqual(t, sc.HasSynced, true)
	_, err := sc.GetClusterTrustBundle("good")
	assert.Error(t, err)
}

func TestClusterTrustBundleHandlers(t *testing.T) {
	type event struct {
		kind     kind.Kind
		name, ns string
	}
	var mu sync.Mutex
	var events []event
	handler := func(k kind.Kind, name, namespace string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event{k, name, namespace})
	}
	seen := func(want event) func() bool {
		return func() bool {
			mu.Lock()
			defer mu.Unlock()
			for _, e := range events {
				if e == want {
					return true
				}
			}
			return false
		}
	}

	client := kube.NewFakeClient()
	clustertrustbundletest.SetAPIAvailable(client)
	stop := test.NewStop(t)
	sc := NewCredentialsController(client, []func(kind.Kind, string, string){handler}, true, stop)
	client.RunAndWait(stop)
	assert.EventuallyEqual(t, sc.HasSynced, true)
	assert.EventuallyEqual(t, sc.clusterTrustBundles.HasSynced, true)

	w := clienttest.NewWriter[*certificatesv1.ClusterTrustBundle](t, client)
	want := event{kind.ClusterTrustBundle, "bundle", ""}
	w.Create(makeCTB("bundle", clustertrustbundletest.GenerateCA(t, "one")))
	assert.EventuallyEqual(t, seen(want), true)

	mu.Lock()
	events = nil
	mu.Unlock()
	w.Update(makeCTB("bundle", clustertrustbundletest.GenerateCA(t, "two")))
	assert.EventuallyEqual(t, seen(want), true)

	mu.Lock()
	events = nil
	mu.Unlock()
	w.Delete("bundle", "")
	assert.EventuallyEqual(t, seen(want), true)
}

func TestClusterTrustBundleMulticluster(t *testing.T) {
	ca := clustertrustbundletest.GenerateCA(t, "ca")
	stop := test.NewStop(t)
	localClient := kube.NewFakeClient(makeCTB("bundle", ca))
	remoteClient := kube.NewFakeClient(makeCTB("remote-only", ca))
	clustertrustbundletest.SetAPIAvailable(localClient)
	clustertrustbundletest.SetAPIAvailable(remoteClient)
	mc := multicluster.NewFakeController()
	sc := NewMulticluster("local", mc)
	mc.Add("local", localClient, stop)
	mc.Add("remote", remoteClient, stop)
	localClient.RunAndWait(stop)
	remoteClient.RunAndWait(stop)

	for _, cl := range []string{"local", "remote"} {
		con, err := sc.ForCluster(cluster.ID(cl))
		assert.NoError(t, err)
		// Always served from the config cluster, whichever cluster the proxy is in.
		assert.EventuallyEqual(t, func() bool {
			info, err := con.GetClusterTrustBundle("bundle")
			return err == nil && string(info.Cert) == ca
		}, true)
		// ClusterTrustBundles of a remote cluster are never read.
		_, err = con.GetClusterTrustBundle("remote-only")
		assert.Error(t, err)
		assert.Equal(t, err.Error(), "clustertrustbundle remote-only not found")
	}
	_, err := (&AggregateController{}).GetClusterTrustBundle("bundle")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "config cluster is not configured")
}

func TestClusterTrustBundleUnreadable(t *testing.T) {
	test.SetForTest(t, &clustertrustbundle.InformerSyncTimeout, 200*time.Millisecond)
	ca := clustertrustbundletest.GenerateCA(t, "ca")
	client := kube.NewFakeClient(makeCTB("good", ca))
	clustertrustbundletest.SetAPIAvailable(client)
	forbidden := atomic.NewBool(true)
	client.Kube().(*fake.Clientset).PrependReactor("list", "clustertrustbundles", func(k8stesting.Action) (bool, runtime.Object, error) {
		if forbidden.Load() {
			return true, nil, errors.NewForbidden(clustertrustbundle.GVR.GroupResource(), "", fmt.Errorf("rbac"))
		}
		return false, nil, nil
	})
	stop := test.NewStop(t)
	sc := NewCredentialsController(client, nil, true, stop)
	// RunAndWait never returns while the informer cannot sync, so it must not block the test.
	go client.RunAndWait(stop)

	// The unsyncable informer must not hold readiness...
	assert.EventuallyEqual(t, sc.HasSynced, true)
	assert.Equal(t, sc.clusterTrustBundles.HasSynced(), false)
	// ...and reading fails closed with an actionable error instead of hanging.
	_, err := sc.GetClusterTrustBundle("good")
	assert.Error(t, err)
	if !strings.Contains(err.Error(), "RBAC") {
		t.Fatalf("error %q does not point at RBAC", err)
	}

	// Once listing works the bundle is served without a restart.
	forbidden.Store(false)
	assert.EventuallyEqual(t, func() bool {
		info, err := sc.GetClusterTrustBundle("good")
		return err == nil && string(info.Cert) == ca
	}, true, retry.Timeout(30*time.Second))
	assert.Equal(t, sc.HasSynced(), true)
}

func TestCloseShutsDownClusterTrustBundleHandlers(t *testing.T) {
	var events atomic.Int32
	client := kube.NewFakeClient()
	clustertrustbundletest.SetAPIAvailable(client)
	stop := test.NewStop(t)
	sc := NewCredentialsController(client, []func(kind.Kind, string, string){
		func(k kind.Kind, _, _ string) {
			if k == kind.ClusterTrustBundle {
				events.Inc()
			}
		},
	}, true, stop)
	client.RunAndWait(stop)
	assert.EventuallyEqual(t, sc.HasSynced, true)

	w := clienttest.NewWriter[*certificatesv1.ClusterTrustBundle](t, client)
	w.Create(makeCTB("bundle", clustertrustbundletest.GenerateCA(t, "one")))
	assert.EventuallyEqual(t, events.Load, int32(1))

	sc.Close()
	w.Update(makeCTB("bundle", clustertrustbundletest.GenerateCA(t, "two")))
	w.Create(makeCTB("other", clustertrustbundletest.GenerateCA(t, "three")))
	assert.Consistently(t, events.Load, int32(1), 200*time.Millisecond)
}
