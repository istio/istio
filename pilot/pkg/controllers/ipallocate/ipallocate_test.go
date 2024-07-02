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

package ipallocate_test

import (
	"net/netip"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/meta/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/controllers/ipallocate"
	"istio.io/istio/pilot/pkg/features"
	autoallocate "istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config/constants"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

type ipAllocateTestRig struct {
	client kubelib.Client
	se     clienttest.TestClient[*networkingv1alpha3.ServiceEntry]
	stop   chan struct{}
	t      *testing.T
}

func setupIPAllocateTest(t *testing.T) (*ipallocate.IPAllocate, ipAllocateTestRig) {
	t.Helper()
	features.EnableV2IPAutoallocate = true
	s := make(chan struct{})
	t.Cleanup(func() { close(s) })
	c := kubelib.NewFakeClient()

	se := clienttest.NewDirectClient[*networkingv1alpha3.ServiceEntry, networkingv1alpha3.ServiceEntry, *networkingv1alpha3.ServiceEntryList](t, c)
	ipController := ipallocate.NewIPAllocate(s, c)
	// these would normally be the first addresses consumed, we should check that warming works by asserting that we get their nexts when we reconcile
	v4 := netip.MustParsePrefix(ipallocate.IPV4Prefix).Addr().Next()
	v6 := netip.MustParsePrefix(ipallocate.IPV6Prefix).Addr().Next()
	kludge := autoallocate.ConditionKludge([]netip.Addr{v4, v6})
	se.CreateOrUpdateStatus(
		&networkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pre-existing",
				Namespace: "default",
			},
			Spec: v1alpha3.ServiceEntry{
				Hosts: []string{
					"test.testing.io",
				},
			},
			Status: v1alpha1.IstioStatus{
				Conditions: []*v1alpha1.IstioCondition{&kludge},
			},
		},
	)
	go ipController.Run(s)
	c.RunAndWait(s)

	return ipController, ipAllocateTestRig{
		client: c,
		se:     se,
		stop:   s,
		t:      t,
	}
}

func TestIPAllocate(t *testing.T) {
	_, rig := setupIPAllocateTest(t)
	rig.se.Create(
		&networkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "with-v4-address",
				Namespace: "boop",
			},
			Spec: v1alpha3.ServiceEntry{
				Hosts: []string{
					"wa-v4.boop.testing.io",
				},
				Addresses: []string{
					"1.2.3.4",
				},
			},
		},
	)
	rig.se.Create(
		&networkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "with-v6-address",
				Namespace: "boop",
			},
			Spec: v1alpha3.ServiceEntry{
				Hosts: []string{
					"wa-v6.boop.testing.io",
				},
				Addresses: []string{
					"1:2::3::4",
				},
			},
		},
	)
	rig.se.Create(
		&networkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "opt-out",
				Namespace: "boop",
				Labels: map[string]string{
					constants.DisableV2AutoAllocationLabel: "true",
				},
			},
			Spec: v1alpha3.ServiceEntry{
				Hosts: []string{
					"nope.boop.testing.io",
				},
			},
		},
	)
	rig.se.Create(
		&networkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "beep",
				Namespace:         "boop",
				CreationTimestamp: metav1.Now(),
			},
			Spec: v1alpha3.ServiceEntry{
				Hosts: []string{
					"beep.boop.testing.io",
				},
			},
			Status: v1alpha1.IstioStatus{
				Conditions: []*v1alpha1.IstioCondition{
					{
						Type:    "test",
						Status:  "asserting",
						Reason:  "controllerTest",
						Message: "this is a test condition, don't overwrite me",
					},
				},
			},
		},
	)
	getter := func() []string {
		addr := autoallocate.GetV2AddressesFromServiceEntry(rig.se.Get("beep", "boop"))
		var res []string
		for _, a := range addr {
			res = append(res, a.String())
		}
		return res
	}
	// this effectively asserts that allocation did work for boop/beep and also that with-address and opt-out did not get addresses
	assert.EventuallyEqual(t, getter, []string{
		netip.MustParsePrefix(ipallocate.IPV4Prefix).Addr().Next().Next().String(),
		netip.MustParsePrefix(ipallocate.IPV6Prefix).Addr().Next().Next().String(),
	}, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))
	assert.Equal(t,
		len(rig.se.Get("beep", "boop").Status.Conditions),
		2,
		"assert that test SE still has 2 conditions")
	assert.Equal(t,
		len(autoallocate.GetV2AddressesFromServiceEntry(rig.se.Get("opt-out", "boop"))),
		0,
		"assert that we did not did not assign addresses when use opted out")
	assert.Equal(t,
		len(autoallocate.GetV2AddressesFromServiceEntry(rig.se.Get("with-v4-address", "boop"))),
		0,
		"assert that we did not assign addresses when user supplied their own")
	assert.Equal(t,
		len(autoallocate.GetV2AddressesFromServiceEntry(rig.se.Get("with-v6-address", "boop"))),
		0,
		"assert that we did not assign addresses when user supplied their own")

	// Testing conflict resolution
	addr := autoallocate.GetV2AddressesFromServiceEntry(rig.se.Get("beep", "boop"))
	rig.se.Create(
		&networkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "user-assigned conflict",
				Namespace: "boop",
			},

			Spec: v1alpha3.ServiceEntry{
				Hosts: []string{
					"in-conflict.boop.testing.io",
				},
				Addresses: []string{
					addr[0].String(),
				},
			},
		},
	)
	// check that this conflict was resolved as expected by allocating new auto-assigned addresses
	assert.EventuallyEqual(t, getter, []string{
		netip.MustParsePrefix(ipallocate.IPV4Prefix).Addr().Next().Next().Next().String(),
		netip.MustParsePrefix(ipallocate.IPV6Prefix).Addr().Next().Next().Next().String(),
	}, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))

	// let's generate an even worse conflict now
	// this is almost certainly caused by some bug in, none the less test we can recover
	status := rig.se.Get("beep", "boop").Status
	rig.se.Create(
		&networkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "status-conflict",
				Namespace:         "boop",
				CreationTimestamp: metav1.Now(),
			},
			Spec: v1alpha3.ServiceEntry{
				Hosts: []string{
					"status-conflict.boop.testing.io",
				},
			},
			Status: status,
		},
	)

	assert.EventuallyEqual(t, func() []string {
		addr := autoallocate.GetV2AddressesFromServiceEntry(rig.se.Get("status-conflict", "boop"))
		var res []string
		for _, a := range addr {
			res = append(res, a.String())
		}
		return res
	}, []string{
		netip.MustParsePrefix(ipallocate.IPV4Prefix).Addr().Next().Next().Next().Next().String(),
		netip.MustParsePrefix(ipallocate.IPV6Prefix).Addr().Next().Next().Next().Next().String(),
	}, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))

	// lets assert that the elder SE doesn't change for 10 consecutive tries
	assert.EventuallyEqual(t, getter, []string{
		netip.MustParsePrefix(ipallocate.IPV4Prefix).Addr().Next().Next().Next().String(),
		netip.MustParsePrefix(ipallocate.IPV6Prefix).Addr().Next().Next().Next().String(),
	}, retry.Converge(10), retry.Delay(time.Millisecond*5))
}
