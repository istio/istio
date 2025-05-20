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
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/api/meta/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/controllers/ipallocate"
	"istio.io/istio/pilot/pkg/features"
	autoallocate "istio.io/istio/pilot/pkg/networking/serviceentry"
	"istio.io/istio/pkg/config/schema/gvr"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	TestIPV4Prefix               = "240.240.0."
	TestIPV6Prefix               = "2001:2::"
	TestNonDefaultIPV4Prefix     = "240.240.2."
	TestNonDefaultIPV6Prefix     = "2001:2:0:2::"
	TestNonDefaultIPV4PrefixCIDR = "240.240.2.0/24"
	TestNonDefaultIPV6PrefixCIDR = "2001:2:0:2::/64"
)

func newV4AddressString(p string, i uint64) string {
	if p == "" {
		p = TestIPV4Prefix
	}
	s := strconv.FormatUint(i, 10)
	return p + s
}

func newV6AddressString(p string, i uint64) string {
	if p == "" {
		p = TestIPV6Prefix
	}
	s := strconv.FormatUint(i, 16)
	return p + s
}

type ipAllocateTestRig struct {
	client kubelib.Client
	se     clienttest.TestClient[*networkingv1alpha3.ServiceEntry]
	stop   chan struct{}
	t      *testing.T
}

func setupIPAllocateTest(t *testing.T, ipv4Prefix, ipv6Prefix string) (*ipallocate.IPAllocator, ipAllocateTestRig) {
	t.Helper()
	s := test.NewStop(t)
	c := kubelib.NewFakeClient()
	clienttest.MakeCRD(t, c, gvr.ServiceEntry)

	// if no prefixes are set, use the default ones
	if ipv4Prefix == "" {
		ipv4Prefix = TestIPV4Prefix
	}
	if ipv6Prefix == "" {
		ipv6Prefix = TestIPV6Prefix
	}

	se := clienttest.NewDirectClient[*networkingv1alpha3.ServiceEntry, networkingv1alpha3.ServiceEntry, *networkingv1alpha3.ServiceEntryList](t, c)
	ipController := ipallocate.NewIPAllocator(s, c)
	// these would normally be the first addresses consumed, we should check that warming works by asserting that we get their nexts when we reconcile
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
				Resolution: v1alpha3.ServiceEntry_DNS,
			},
			Status: v1alpha3.ServiceEntryStatus{Addresses: []*v1alpha3.ServiceEntryAddress{
				{
					Host:  "test.testing.io",
					Value: newV4AddressString(ipv4Prefix, 1),
				},
				{
					Host:  "test.testing.io",
					Value: newV6AddressString(ipv6Prefix, 1),
				},
			}},
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
	_, rig := setupIPAllocateTest(t, TestIPV4Prefix, TestIPV6Prefix)
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
				Resolution: v1alpha3.ServiceEntry_DNS,
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
				Resolution: v1alpha3.ServiceEntry_DNS,
			},
		},
	)
	rig.se.Create(
		&networkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "opt-out",
				Namespace: "boop",
				Labels: map[string]string{
					label.NetworkingEnableAutoallocateIp.Name: "false",
				},
			},
			Spec: v1alpha3.ServiceEntry{
				Hosts: []string{
					"nope.boop.testing.io",
				},
				Resolution: v1alpha3.ServiceEntry_DNS,
			},
		},
	)
	rig.se.Create(
		&networkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "with-existing-status",
				Namespace:         "boop",
				CreationTimestamp: metav1.Now(),
			},
			Spec: v1alpha3.ServiceEntry{
				Hosts: []string{
					"with-existing-status.boop.testing.io",
				},
				Resolution: v1alpha3.ServiceEntry_DNS,
			},
			Status: v1alpha3.ServiceEntryStatus{
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
		addr := autoallocate.GetAddressesFromServiceEntry(rig.se.Get("with-existing-status", "boop"))
		var res []string
		for _, a := range addr {
			res = append(res, a.String())
		}
		return res
	}
	// this effectively asserts that allocation did work for boop/beep and also that with-address and opt-out did not get addresses
	assert.EventuallyEqual(t, getter, []string{
		newV4AddressString(TestIPV4Prefix, 2),
		newV6AddressString(TestIPV6Prefix, 2),
	}, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))
	assert.Equal(t,
		len(rig.se.Get("with-existing-status", "boop").Status.GetConditions()),
		1,
		"assert that test SE still has its condition")
	assert.Equal(t,
		len(autoallocate.GetHostAddressesFromServiceEntry(rig.se.Get("opt-out", "boop"))),
		0,
		"assert that we did not did not assign addresses when use opted out")
	assert.Equal(t,
		len(autoallocate.GetHostAddressesFromServiceEntry(rig.se.Get("with-v4-address", "boop"))),
		0,
		"assert that we did not assign addresses when user supplied their own")
	assert.Equal(t,
		len(autoallocate.GetHostAddressesFromServiceEntry(rig.se.Get("with-v6-address", "boop"))),
		0,
		"assert that we did not assign addresses when user supplied their own")

	// Testing conflict resolution
	addr := autoallocate.GetAddressesFromServiceEntry(rig.se.Get("with-existing-status", "boop"))
	assert.Equal(t, len(addr), 2, "ensure we retrieved addresses to create a conflict with")
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
				Resolution: v1alpha3.ServiceEntry_DNS,
			},
		},
	)
	// Assert this conflict was resolved as expected by allocating new auto-assigned addresses
	assert.EventuallyEqual(t, getter, []string{
		newV4AddressString(TestIPV4Prefix, 3),
		newV6AddressString(TestIPV6Prefix, 3),
	}, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))

	// Assert that we are not mutating user spec during resolution of conflicts
	assert.EventuallyEqual(
		t,
		func() []string { return rig.se.Get("user-assigned conflict", "boop").Spec.Addresses },
		[]string{addr[0].String()},
		retry.Converge(10),
	)
	assert.Equal(
		t,
		len(rig.se.Get("user-assigned conflict", "boop").Spec.Addresses),
		1,
		"assert that we did not add to spec.addresses during conflict resolution",
	)

	// let's generate an even worse conflict now
	// this is almost certainly caused by some bug in our code, none the less test we can recover
	conflictingAddresses := autoallocate.GetAddressesFromServiceEntry(rig.se.Get("with-existing-status", "boop"))
	assert.Equal(t, len(conflictingAddresses), 2, "ensure we retrieved addresses to create a conflict with")
	conflictingStatusAddresses := []*v1alpha3.ServiceEntryAddress{}
	statusConflictHost := "status-conflict.boop.testing.io"
	for _, a := range conflictingAddresses {
		conflictingStatusAddresses = append(conflictingStatusAddresses, &v1alpha3.ServiceEntryAddress{Value: a.String(), Host: statusConflictHost})
	}
	// create an auto-assigned conflict
	rig.se.Create(
		&networkingv1alpha3.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "status-conflict",
				Namespace:         "boop",
				CreationTimestamp: metav1.Now(),
			},
			Spec: v1alpha3.ServiceEntry{
				Hosts: []string{
					statusConflictHost,
				},
				Resolution: v1alpha3.ServiceEntry_DNS,
			},
			Status: v1alpha3.ServiceEntryStatus{
				Addresses: conflictingStatusAddresses,
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

	// assert that conflicts are resolved on the newer SE
	assert.EventuallyEqual(t, func() []string {
		addr := autoallocate.GetAddressesFromServiceEntry(rig.se.Get("status-conflict", "boop"))
		var res []string
		for _, a := range addr {
			res = append(res, a.String())
		}
		return res
	}, []string{
		newV4AddressString(TestIPV4Prefix, 4),
		newV6AddressString(TestIPV6Prefix, 4),
	}, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))

	// assert that resolving conflicts does not destroy existing status items
	assert.Equal(t,
		len(rig.se.Get("status-conflict", "boop").Status.GetConditions()),
		1,
		"assert that status-conflict still has its condition")

	// assert that the elder SE doesn't change for 10 consecutive tries
	assert.EventuallyEqual(t, getter, []string{
		newV4AddressString(TestIPV4Prefix, 3),
		newV6AddressString(TestIPV6Prefix, 3),
	}, retry.Converge(10), retry.Delay(time.Millisecond*5))

	// test that adding to the list of hosts produces the correct host to IP mapping
	se := rig.se.Get("pre-existing", "default")
	se.Spec.Hosts = append(se.Spec.Hosts, "added.testing.io")
	rig.se.Update(se)
	assert.EventuallyEqual(t, func() int {
		se := rig.se.Get("pre-existing", "default")
		return len(autoallocate.GetAddressesFromServiceEntry(se))
	}, 4, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))
	assert.Equal(t, toMapStringString(autoallocate.GetHostAddressesFromServiceEntry(rig.se.Get("pre-existing", "default"))),
		map[string][]string{
			"test.testing.io": {
				newV4AddressString(TestIPV4Prefix, 1),
				newV6AddressString(TestIPV6Prefix, 1),
			},
			"added.testing.io": {
				newV4AddressString(TestIPV4Prefix, 5),
				newV6AddressString(TestIPV6Prefix, 5),
			},
		},
	)

	// test that adding a wildcard host to the list of hosts does not allocate an ip for it
	se = rig.se.Get("pre-existing", "default")
	se.Spec.Hosts = append(se.Spec.Hosts, "*.bad-wildcard.testing.io")
	rig.se.Update(se)
	assert.EventuallyEqual(t, func() int {
		se := rig.se.Get("pre-existing", "default")
		return len(autoallocate.GetAddressesFromServiceEntry(se))
	}, 4, retry.Converge(10), retry.Delay(time.Millisecond*5))
	assert.Equal(t, toMapStringString(autoallocate.GetHostAddressesFromServiceEntry(rig.se.Get("pre-existing", "default"))),
		map[string][]string{
			"test.testing.io": {
				newV4AddressString(TestIPV4Prefix, 1),
				newV6AddressString(TestIPV6Prefix, 1),
			},
			"added.testing.io": {
				newV4AddressString(TestIPV4Prefix, 5),
				newV6AddressString(TestIPV6Prefix, 5),
			},
		},
	)

	// reset pre-existing to single host
	se = rig.se.Get("pre-existing", "default")
	se.Spec.Hosts = []string{"test.testing.io"}
	rig.se.Update(se)
	assert.EventuallyEqual(t, func() int {
		se := rig.se.Get("pre-existing", "default")
		return len(autoallocate.GetAddressesFromServiceEntry(se))
	}, 2, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))

	// check that adding lots of duplicate host entries at once does not allocate new IPs for each
	se = rig.se.Get("pre-existing", "default")
	se.Spec.Hosts = append(se.Spec.Hosts, []string{
		"added.testing.io",
		"added.testing.io",
		"added.testing.io",
		"added.testing.io",
	}...)
	rig.se.Update(se)
	assert.EventuallyEqual(t, func() int {
		se := rig.se.Get("pre-existing", "default")
		return len(autoallocate.GetAddressesFromServiceEntry(se))
	}, 4, retry.Converge(10), retry.Delay(time.Millisecond*5))
	assert.Equal(t, toMapStringString(autoallocate.GetHostAddressesFromServiceEntry(rig.se.Get("pre-existing", "default"))),
		map[string][]string{
			"test.testing.io": {
				newV4AddressString(TestIPV4Prefix, 1),
				newV6AddressString(TestIPV6Prefix, 1),
			},
			"added.testing.io": {
				newV4AddressString(TestIPV4Prefix, 6),
				newV6AddressString(TestIPV6Prefix, 6),
			},
		},
	)

	// test that removing from the list of hosts produces the correct host to IP mapping
	se = rig.se.Get("pre-existing", "default")
	se.Spec.Hosts = []string{"added.testing.io"}
	rig.se.Update(se)
	assert.EventuallyEqual(t, func() int {
		se := rig.se.Get("pre-existing", "default")
		return len(autoallocate.GetAddressesFromServiceEntry(se))
	}, 2, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))
	assert.Equal(t, toMapStringString(autoallocate.GetHostAddressesFromServiceEntry(rig.se.Get("pre-existing", "default"))),
		map[string][]string{
			"added.testing.io": {
				newV4AddressString(TestIPV4Prefix, 6),
				newV6AddressString(TestIPV6Prefix, 6),
			},
		},
	)

	// a big add + remove to list of hosts produces the correct host to IP mapping
	se = rig.se.Get("pre-existing", "default")
	se.Spec.Hosts = []string{
		"seven.testing.io",
		"eight.testing.io",
		"nine.testing.io",
		"A.testing.io",
	}
	rig.se.Update(se)
	assert.EventuallyEqual(t, func() int {
		se := rig.se.Get("pre-existing", "default")
		return len(autoallocate.GetAddressesFromServiceEntry(se))
	}, 8, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))
	assert.Equal(t, toMapStringString(autoallocate.GetHostAddressesFromServiceEntry(rig.se.Get("pre-existing", "default"))),
		map[string][]string{
			"seven.testing.io": {
				newV4AddressString(TestIPV4Prefix, 7),
				newV6AddressString(TestIPV6Prefix, 7),
			},
			"eight.testing.io": {
				newV4AddressString(TestIPV4Prefix, 8),
				newV6AddressString(TestIPV6Prefix, 8),
			},
			"nine.testing.io": {
				newV4AddressString(TestIPV4Prefix, 9),
				newV6AddressString(TestIPV6Prefix, 9),
			},
			"A.testing.io": {
				newV4AddressString(TestIPV4Prefix, 10),
				newV6AddressString(TestIPV6Prefix, 10),
			},
		},
	)

	// for completeness test that having no hosts produces an empty map
	se = rig.se.Get("pre-existing", "default")
	se.Spec.Hosts = []string{}
	rig.se.Update(se)
	assert.EventuallyEqual(t, func() int {
		se := rig.se.Get("pre-existing", "default")
		return len(autoallocate.GetAddressesFromServiceEntry(se))
	}, 0, retry.MaxAttempts(10), retry.Delay(time.Millisecond*5))
	assert.Equal(t, toMapStringString(autoallocate.GetHostAddressesFromServiceEntry(rig.se.Get("pre-existing", "default"))),
		map[string][]string{})
}

func TestIPAllocateWithEnvCIDR(t *testing.T) {
	features.IPAutoallocateIPv4Prefix = TestNonDefaultIPV4PrefixCIDR
	features.IPAutoallocateIPv6Prefix = TestNonDefaultIPV6PrefixCIDR
	_, rig := setupIPAllocateTest(t, TestNonDefaultIPV4Prefix, TestNonDefaultIPV6Prefix)
	se := rig.se.Get("pre-existing", "default")
	// test that the non-default CIDR prefixes are used
	se.Spec.Hosts = append(se.Spec.Hosts, "added.testing.io")
	rig.se.Update(se)
	assert.EventuallyEqual(t, func() int {
		se := rig.se.Get("pre-existing", "default")
		return len(autoallocate.GetAddressesFromServiceEntry(se))
	}, 4, retry.Converge(10), retry.Delay(time.Millisecond*5))
	assert.Equal(t, toMapStringString(autoallocate.GetHostAddressesFromServiceEntry(rig.se.Get("pre-existing", "default"))),
		map[string][]string{
			"test.testing.io": {
				newV4AddressString(TestNonDefaultIPV4Prefix, 1),
				newV6AddressString(TestNonDefaultIPV6Prefix, 1),
			},
			"added.testing.io": {
				newV4AddressString(TestNonDefaultIPV4Prefix, 2),
				newV6AddressString(TestNonDefaultIPV6Prefix, 2),
			},
		},
		"assert that we assigned the next address in the range to the new host",
	)
}

func toMapStringString(in map[string][]netip.Addr) map[string][]string {
	out := map[string][]string{}

	for k, v := range in {
		for _, ip := range v {
			out[k] = append(out[k], ip.String())
		}
	}

	return out
}
