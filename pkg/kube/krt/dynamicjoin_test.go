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

package krt_test

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/api/networking/v1alpha3"
	istio "istio.io/api/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/serviceentry"
	srkube "istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func TestDynamicJoinCollection(t *testing.T) {
	opts := testOptions(t)
	c1 := krt.NewStatic[Named](nil, true, opts.WithName("c1")...)
	c2 := krt.NewStatic[Named](nil, true, opts.WithName("c2")...)
	c3 := krt.NewStatic[Named](nil, true, opts.WithName("c3")...)
	dj := krt.DynamicJoinCollection(
		[]krt.Collection[Named]{c1.AsCollection(), c2.AsCollection(), c3.AsCollection()},
		opts.WithName("DynamicJoin")...,
	)

	last := atomic.NewString("")
	dj.Register(func(o krt.Event[Named]) {
		last.Store(o.Latest().ResourceName())
	})

	assert.EventuallyEqual(t, last.Load, "")
	c1.Set(&Named{"c1", "a"})
	assert.EventuallyEqual(t, last.Load, "c1/a")

	c2.Set(&Named{"c2", "a"})
	assert.EventuallyEqual(t, last.Load, "c2/a")

	c3.Set(&Named{"c3", "a"})
	assert.EventuallyEqual(t, last.Load, "c3/a")

	c1.Set(&Named{"c1", "b"})
	assert.EventuallyEqual(t, last.Load, "c1/b")
	// ordered by c1, c2, c3
	sortf := func(a Named) string {
		return a.ResourceName()
	}
	assert.Equal(
		t,
		slices.SortBy(dj.List(), sortf),
		slices.SortBy([]Named{
			{"c1", "b"},
			{"c2", "a"},
			{"c3", "a"},
		}, sortf),
	)

	// add c4
	c4 := krt.NewStatic[Named](nil, true, opts.WithName("c4")...)
	dj.AddOrUpdateCollection(c4.AsCollection())
	c4.Set(&Named{"c4", "a"})
	assert.EventuallyEqual(t, last.Load, "c4/a") // Test that events from the new collection make it to the join

	// remove c1
	dj.RemoveCollection(c1.AsCollection())
	c1.Set(&Named{"c1", "c"})
	assert.EventuallyEqual(t, last.Load, "c4/a") // Test that events from removed collections do not make it to the join
}

func TestDynamicJoinCollectionIndex(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c1 := kube.NewFakeClient()
	kpc1 := kclient.New[*corev1.Pod](c1)
	pc1 := clienttest.Wrap(t, kpc1)
	pods := krt.WrapClient[*corev1.Pod](kpc1, opts.WithName("Pods1")...)
	c1.RunAndWait(stop)
	SimplePods1 := NamedSimplePodCollection(pods, opts, "Pods1")
	SimpleGlobalPods := krt.DynamicJoinCollection(
		[]krt.Collection[SimplePod]{SimplePods1},
		opts.WithName("GlobalPods")...,
	)
	tt := assert.NewTracker[string](t)
	IPIndex := krt.NewIndex[string, SimplePod](SimpleGlobalPods, func(o SimplePod) []string {
		return []string{o.IP}
	})
	fetchSorted := func(ip string) []SimplePod {
		return slices.SortBy(IPIndex.Lookup(ip), func(t SimplePod) string {
			return t.ResourceName()
		})
	}

	SimpleGlobalPods.Register(TrackerHandler[SimplePod](tt))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	}
	pc1.CreateOrUpdateStatus(pod)
	tt.WaitUnordered("add/namespace/name")
	assert.Equal(t, fetchSorted("1.2.3.4"), []SimplePod{{NewNamed(pod), Labeled{}, "1.2.3.4"}})

	c2 := kube.NewFakeClient()
	kpc2 := kclient.New[*corev1.Pod](c2)
	pc2 := clienttest.Wrap(t, kpc2)
	pods2 := krt.WrapClient[*corev1.Pod](kpc2, opts.WithName("Pods2")...)
	c2.RunAndWait(stop)
	SimplePods2 := NamedSimplePodCollection(pods2, opts, "Pods2")

	SimpleGlobalPods.AddOrUpdateCollection(SimplePods2)
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	}
	pc2.CreateOrUpdateStatus(pod2)
	tt.WaitUnordered("add/namespace/name2")
	assert.Equal(t, fetchSorted("1.2.3.5"), []SimplePod{{NewNamed(pod2), Labeled{}, "1.2.3.5"}})

	// remove c1
	SimpleGlobalPods.RemoveCollection(SimplePods1)
	tt.WaitUnordered("delete/namespace/name")
	assert.Equal(t, fetchSorted("1.2.3.4"), []SimplePod{})
}

func TestDynamicCollectionJoinSync(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	})
	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := NamedSimplePodCollection(pods, opts, "Pods")
	ExtraSimplePods := krt.NewStatic(&SimplePod{
		Named:   Named{"namespace", "name-static"},
		Labeled: Labeled{map[string]string{"app": "foo"}},
		IP:      "9.9.9.9",
	}, true, opts.WithName("Simple")...)
	AllPods := krt.DynamicJoinCollection(
		[]krt.Collection[SimplePod]{SimplePods, ExtraSimplePods.AsCollection()},
		opts.WithName("AllPods")...,
	)
	assert.Equal(t, AllPods.WaitUntilSynced(stop), true)
	// Assert Equal -- not EventuallyEqual -- to ensure our WaitForCacheSync is proper
	assert.Equal(t, fetcherSorted(AllPods)(), []SimplePod{
		{Named{"namespace", "name"}, NewLabeled(map[string]string{"app": "foo"}), "1.2.3.4"},
		{Named{"namespace", "name-static"}, NewLabeled(map[string]string{"app": "foo"}), "9.9.9.9"},
	})

	c2 := kube.NewFakeClient(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "bar"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	})
	pods2 := krt.NewInformer[*corev1.Pod](c2, opts.WithName("Pods2")...)
	SimplePods2 := NamedSimplePodCollection(pods2, opts, "Pods2")
	ExtraSimplePods2 := krt.NewStatic(&SimplePod{
		Named:   Named{"namespace", "name2-static"},
		Labeled: Labeled{map[string]string{"app": "bar"}},
		IP:      "9.9.9.8",
	}, true, opts.WithName("Simple2")...)
	AllPods.AddOrUpdateCollection(ExtraSimplePods2.AsCollection())
	AllPods.AddOrUpdateCollection(SimplePods2)
	// Should be false since we never ran the fake client
	// Use a context for a convenient timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel() // make the linter happy
	assert.Equal(t, AllPods.WaitUntilSynced(ctx.Done()), false)
	c2.RunAndWait(stop)
	assert.Equal(t, AllPods.WaitUntilSynced(stop), true)
	// Assert Equal -- not EventuallyEqual -- to ensure our WaitForCacheSync is proper
	assert.Equal(t, fetcherSorted(AllPods)(), []SimplePod{
		{Named{"namespace", "name"}, NewLabeled(map[string]string{"app": "foo"}), "1.2.3.4"},
		{Named{"namespace", "name-static"}, NewLabeled(map[string]string{"app": "foo"}), "9.9.9.9"},
		{Named{"namespace", "name2"}, NewLabeled(map[string]string{"app": "bar"}), "1.2.3.5"},
		{Named{"namespace", "name2-static"}, NewLabeled(map[string]string{"app": "bar"}), "9.9.9.8"},
	})

	AllPods.RemoveCollection(SimplePods2)
	assert.Equal(t, fetcherSorted(AllPods)(), []SimplePod{
		{Named{"namespace", "name"}, NewLabeled(map[string]string{"app": "foo"}), "1.2.3.4"},
		{Named{"namespace", "name-static"}, NewLabeled(map[string]string{"app": "foo"}), "9.9.9.9"},
		{Named{"namespace", "name2-static"}, NewLabeled(map[string]string{"app": "bar"}), "9.9.9.8"},
	})
}

func TestDynamicJoinTransform(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	services1 := krt.NewInformer[*corev1.Service](c, opts.WithName("Services")...)
	serviceEntries1 := krt.NewInformer[*istioclient.ServiceEntry](c, opts.WithName("ServiceEntries")...)
	AllServices := krt.DynamicJoinCollection(
		[]krt.Collection[*corev1.Service]{services1},
		opts.WithName("AllServices")...)
	AllServiceEntries := krt.DynamicJoinCollection(
		[]krt.Collection[*istioclient.ServiceEntry]{serviceEntries1},
		opts.WithName("AllServiceEntries")...,
	)

	c.RunAndWait(stop)
	sc := clienttest.Wrap(t, kclient.New[*corev1.Service](c))

	serviceBuilder := func() krt.TransformationSingle[*corev1.Service, model.ServiceInfo] {
		return func(ctx krt.HandlerContext, s *corev1.Service) *model.ServiceInfo {
			if s.Spec.Type == v1.ServiceTypeExternalName {
				// ExternalName services are not implemented by ambient (but will still work).
				// The DNS requests will forward to the upstream DNS server, then Ztunnel can handle the request based on the target
				// hostname.
				// In theory we could add support for native 'DNS alias' into Ztunnel's DNS proxy. This would give the same behavior
				// but let the DNS proxy handle it instead of forwarding upstream. However, at this time we do not do so.
				return nil
			}
			portNames := map[int32]model.ServicePortName{}
			for _, p := range s.Spec.Ports {
				portNames[p.Port] = model.ServicePortName{
					PortName:       p.Name,
					TargetPortName: p.TargetPort.StrVal,
				}
			}
			svc := constructService(ctx, s)
			return &model.ServiceInfo{
				Service:       svc,
				PortNames:     portNames,
				LabelSelector: model.NewSelector(s.Spec.Selector),
				Source:        ambient.MakeSource(s),
			}
		}
	}

	serviceEntriesBuilder := func() krt.TransformationMulti[*istioclient.ServiceEntry, model.ServiceInfo] {
		return func(ctx krt.HandlerContext, se *istioclient.ServiceEntry) []model.ServiceInfo {
			sel := model.NewSelector(se.Spec.GetWorkloadSelector().GetLabels())
			portNames := map[int32]model.ServicePortName{}
			for _, p := range se.Spec.Ports {
				portNames[int32(p.Number)] = model.ServicePortName{
					PortName: p.Name,
				}
			}
			return slices.Map(constructServiceEntries(ctx, se), func(e *workloadapi.Service) model.ServiceInfo {
				return model.ServiceInfo{
					Service:       e,
					PortNames:     portNames,
					LabelSelector: sel,
					Source:        ambient.MakeSource(se),
				}
			})
		}
	}
	ServicesInfo := krt.NewCollection(AllServices, serviceBuilder(), opts.WithName("ServicesInfo")...)
	ServiceEntriesInfo := krt.NewManyCollection(AllServiceEntries, serviceEntriesBuilder(), opts.WithName("ServiceEntriesInfo")...)
	WorkloadServices := krt.JoinCollection(
		[]krt.Collection[model.ServiceInfo]{ServicesInfo, ServiceEntriesInfo},
		opts.WithName("WorkloadServices")...,
	)

	ServiceAddressIndex := krt.NewIndex[networkAddress, model.ServiceInfo](WorkloadServices, networkAddressFromService)
	assert.Equal(t, ServiceAddressIndex.Lookup(networkAddress{"testnetwork", "1.2.3.4"}), nil)

	// Create a new service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "foo"},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			ClusterIP: "1.2.3.4",
		},
	}
	sc.Create(svc)

	// The ServiceInfo should arrive in the index
	assert.EventuallyEqual(t, func() []model.ServiceInfo {
		return slices.SortBy(ServiceAddressIndex.Lookup(networkAddress{"testnetwork", "1.2.3.4"}), func(s model.ServiceInfo) string {
			return s.Service.Hostname
		})
	}, []model.ServiceInfo{
		{
			Service: &workloadapi.Service{
				Name:      "svc",
				Namespace: "namespace",
				Hostname:  string(srkube.ServiceHostname("svc", "namespace", "test.local")),
				Addresses: []*workloadapi.NetworkAddress{
					{
						Network: "testnetwork",
						Address: netip.MustParseAddr("1.2.3.4").AsSlice(),
					},
				},
				Ports: []*workloadapi.Port{
					{
						ServicePort: uint32(80),
						TargetPort:  uint32(8080),
					},
				},
			},
			LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
			PortNames: map[int32]model.ServicePortName{
				80: {
					PortName: "http",
				},
			},
			Source: model.TypedObject{
				Kind: kind.Service,
				NamespacedName: types.NamespacedName{
					Name:      "svc",
					Namespace: "namespace",
				},
			},
		},
	})
	assert.Equal(t, ServiceAddressIndex.Lookup(networkAddress{"testnetwork", "1.2.3.5"}), nil)

	// Add new fake client
	c2 := kube.NewFakeClient()
	serviceEntries2 := krt.NewInformer[*istioclient.ServiceEntry](c2, opts.WithName("ServiceEntries2")...)
	c2.RunAndWait(stop)
	AllServiceEntries.AddOrUpdateCollection(serviceEntries2)
	sec2 := clienttest.Wrap(t, kclient.New[*istioclient.ServiceEntry](c2))
	se2 := &istioclient.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-entry",
			Namespace: "namespace",
		},
		Spec: istio.ServiceEntry{
			WorkloadSelector: &istio.WorkloadSelector{
				Labels: map[string]string{"app": "foo"},
			},
			Hosts:     []string{"svc-entry.namespace.svc.test.local"},
			Addresses: []string{"1.2.3.5"},
			Ports: []*istio.ServicePort{
				{
					Number:     80,
					Name:       "http",
					Protocol:   "http",
					TargetPort: 8080,
				},
			},
		},
	}
	sec2.Create(se2)
	// We should see new ServiceInfo in the index
	assert.EventuallyEqual(t, func() []model.ServiceInfo {
		return slices.SortBy(ServiceAddressIndex.Lookup(networkAddress{"testnetwork", "1.2.3.5"}), func(s model.ServiceInfo) string {
			return s.Service.Hostname
		})
	}, []model.ServiceInfo{
		{
			Service: &workloadapi.Service{
				Name:      "svc-entry",
				Namespace: "namespace",
				Hostname:  string(srkube.ServiceHostname("svc-entry", "namespace", "test.local")),
				Addresses: []*workloadapi.NetworkAddress{
					{
						Network: "testnetwork",
						Address: netip.MustParseAddr("1.2.3.5").AsSlice(),
					},
				},
				Ports: []*workloadapi.Port{
					{
						ServicePort: uint32(80),
						TargetPort:  uint32(8080),
					},
				},
				LoadBalancing: &workloadapi.LoadBalancing{},
			},
			LabelSelector: model.NewSelector(map[string]string{"app": "foo"}),
			PortNames: map[int32]model.ServicePortName{
				80: {
					PortName: "http",
				},
			},
			Source: model.TypedObject{
				Kind: kind.ServiceEntry,
				NamespacedName: types.NamespacedName{
					Name:      "svc-entry",
					Namespace: "namespace",
				},
			},
		},
	})
}

func constructService(ctx krt.HandlerContext, svc *corev1.Service) *workloadapi.Service {
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: uint32(p.Port),
			TargetPort:  uint32(p.TargetPort.IntVal),
		})
	}

	addresses, err := slices.MapErr(getVIPs(svc), func(vip string) (*workloadapi.NetworkAddress, error) {
		ip, err := netip.ParseAddr(vip)
		if err != nil {
			return nil, fmt.Errorf("parse %v: %v", vip, err)
		}
		return &workloadapi.NetworkAddress{
			Network: "testnetwork",
			Address: ip.AsSlice(),
		}, nil
	})
	if err != nil {
		log.Warnf("fail to parse service %v: %v", config.NamespacedName(svc), err)
		return nil
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	return &workloadapi.Service{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Hostname:  string(srkube.ServiceHostname(svc.Name, svc.Namespace, "test.local")),
		Addresses: addresses,
		Ports:     ports,
	}
}

func constructServiceEntries(ctx krt.HandlerContext, svc *istioclient.ServiceEntry) []*workloadapi.Service {
	var autoassignedHostAddresses map[string][]netip.Addr
	addresses, err := slices.MapErr(svc.Spec.Addresses, func(vip string) (*workloadapi.NetworkAddress, error) {
		ip, err := parseCidrOrIP(vip)
		if err != nil {
			return nil, err
		}
		return &workloadapi.NetworkAddress{
			Network: "testnetwork",
			Address: ip.AsSlice(),
		}, nil
	})
	if err != nil {
		return nil
	}
	// if this se has autoallocation we can se autoallocated IP, otherwise it will remain an empty slice
	if serviceentry.ShouldV2AutoAllocateIP(svc) {
		autoassignedHostAddresses = serviceentry.GetHostAddressesFromServiceEntry(svc)
	}
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		target := p.TargetPort
		if target == 0 {
			target = p.Number
		}
		ports = append(ports, &workloadapi.Port{
			ServicePort: p.Number,
			TargetPort:  target,
		})
	}

	lb := &workloadapi.LoadBalancing{}

	res := make([]*workloadapi.Service, 0, len(svc.Spec.Hosts))
	for _, h := range svc.Spec.Hosts {
		// if we have no user-provided hostsAddresses and h is not wildcarded and we have hostsAddresses supported resolution
		// we can try to use autoassigned hostsAddresses
		hostsAddresses := addresses
		if len(hostsAddresses) == 0 && !host.Name(h).IsWildCarded() && svc.Spec.Resolution != v1alpha3.ServiceEntry_NONE {
			if hostsAddrs, ok := autoassignedHostAddresses[h]; ok {
				hostsAddresses = slices.Map(hostsAddrs, func(ip netip.Addr) *workloadapi.NetworkAddress {
					return &workloadapi.NetworkAddress{
						Network: "testnetwork",
						Address: ip.AsSlice(),
					}
				})
			}
		}
		res = append(res, &workloadapi.Service{
			Name:            svc.Name,
			Namespace:       svc.Namespace,
			Hostname:        h,
			Addresses:       hostsAddresses,
			Ports:           ports,
			SubjectAltNames: svc.Spec.SubjectAltNames,
			LoadBalancing:   lb,
		})
	}
	return res
}

func getVIPs(svc *corev1.Service) []string {
	res := []string{}
	cips := svc.Spec.ClusterIPs
	if len(cips) == 0 {
		cips = []string{svc.Spec.ClusterIP}
	}
	for _, cip := range cips {
		if cip != "" && cip != v1.ClusterIPNone {
			res = append(res, cip)
		}
	}
	return res
}

func networkAddressFromService(s model.ServiceInfo) []networkAddress {
	networkAddrs := make([]networkAddress, 0, len(s.Service.Addresses))
	for _, addr := range s.Service.Addresses {
		// mustByteIPToString is ok since this is from our IP constructed
		networkAddrs = append(networkAddrs, networkAddress{network: addr.Network, ip: mustByteIPToString(addr.Address)})
	}
	return networkAddrs
}

type networkAddress struct {
	network string
	ip      string
}

func (n networkAddress) String() string {
	return n.network + "/" + n.ip
}

func parseCidrOrIP(ip string) (netip.Addr, error) {
	if strings.Contains(ip, "/") {
		prefix, err := netip.ParsePrefix(ip)
		if err != nil {
			return netip.Addr{}, err
		}
		if !prefix.IsSingleIP() {
			return netip.Addr{}, fmt.Errorf("only single IP CIDR is allowed")
		}
		return prefix.Addr(), nil
	}
	return netip.ParseAddr(ip)
}

func mustByteIPToString(b []byte) string {
	ip, _ := netip.AddrFromSlice(b) // Address only comes from objects we create, so it must be valid
	return ip.String()
}
