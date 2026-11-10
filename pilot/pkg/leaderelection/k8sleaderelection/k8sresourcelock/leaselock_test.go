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

package k8sresourcelock

import (
	"context"
	"testing"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pkg/test/util/assert"
)

const (
	testNamespace = "istio-system"
	testLeaseName = "istio-gateway-deployment-canary"
)

func newTestLeaseLock(client *fake.Clientset, labels map[string]string) *LeaseLock {
	return &LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Namespace: testNamespace, Name: testLeaseName},
		Client:     client.CoordinationV1(),
		LockConfig: ResourceLockConfig{Identity: "istiod-1"},
		Labels:     labels,
	}
}

func getTestLease(t *testing.T, client *fake.Clientset) *coordinationv1.Lease {
	t.Helper()
	lease, err := client.CoordinationV1().Leases(testNamespace).Get(context.Background(), testLeaseName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestLeaseLockCreateSetsLabels(t *testing.T) {
	client := fake.NewClientset()
	labels := map[string]string{"istio.io/rev": "canary", "operator.istio.io/component": "Pilot"}
	ll := newTestLeaseLock(client, labels)

	if err := ll.Create(context.Background(), LeaderElectionRecord{HolderIdentity: "istiod-1", HolderKey: "canary"}); err != nil {
		t.Fatal(err)
	}

	lease := getTestLease(t, client)
	assert.Equal(t, lease.Labels, labels)
	assert.Equal(t, lease.Annotations[LeaseHolderKey], "canary")
	assert.Equal(t, *lease.Spec.HolderIdentity, "istiod-1")
}

func TestLeaseLockCreateWithoutLabels(t *testing.T) {
	client := fake.NewClientset()
	ll := newTestLeaseLock(client, nil)

	if err := ll.Create(context.Background(), LeaderElectionRecord{HolderIdentity: "istiod-1"}); err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, len(getTestLease(t, client).Labels), 0)
}

func TestLeaseLockUpdateMergesLabels(t *testing.T) {
	// The Lease already exists without Istio labels, as it would after an in-place upgrade from an istiod
	// version that did not label Leases. It also carries an unrelated label that must be kept.
	existing := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testLeaseName,
			Labels:    map[string]string{"keep": "me"},
		},
	}
	client := fake.NewClientset(existing)
	ll := newTestLeaseLock(client, map[string]string{"istio.io/rev": "canary", "operator.istio.io/component": "Pilot"})

	if _, _, err := ll.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ll.Update(context.Background(), LeaderElectionRecord{HolderIdentity: "istiod-1", HolderKey: "canary"}); err != nil {
		t.Fatal(err)
	}

	lease := getTestLease(t, client)
	assert.Equal(t, lease.Labels, map[string]string{
		"keep":                        "me",
		"istio.io/rev":                "canary",
		"operator.istio.io/component": "Pilot",
	})
	assert.Equal(t, lease.Annotations[LeaseHolderKey], "canary")
	assert.Equal(t, *lease.Spec.HolderIdentity, "istiod-1")
}

func TestLeaseLockUpdateWithoutLabelsKeepsExistingLabels(t *testing.T) {
	existing := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testLeaseName,
			Labels:    map[string]string{"keep": "me"},
		},
	}
	client := fake.NewClientset(existing)
	ll := newTestLeaseLock(client, nil)

	if _, _, err := ll.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ll.Update(context.Background(), LeaderElectionRecord{HolderIdentity: "istiod-1"}); err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, getTestLease(t, client).Labels, map[string]string{"keep": "me"})
}
