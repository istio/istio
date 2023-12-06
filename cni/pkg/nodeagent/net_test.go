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

package nodeagent

import (
	"context"
	"errors"
	"net/netip"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/istio/cni/pkg/ipset"
	"istio.io/istio/cni/pkg/iptables"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func setupLogging() {
	opts := istiolog.DefaultOptions()
	opts.SetOutputLevel(istiolog.OverrideScopeName, istiolog.DebugLevel)
	istiolog.Configure(opts)
	for _, scope := range istiolog.Scopes() {
		scope.SetOutputLevel(istiolog.DebugLevel)
	}
}

type netTestFixture struct {
	netServer            *NetServer
	podNsMap             *podNetnsCache
	ztunnelServer        *fakeZtunnel
	iptablesConfigurator *iptables.IptablesConfigurator
	nlDeps               *fakeIptablesDeps
	ipsetDeps            *ipset.MockedIpsetDeps
}

func getTestFixure(ctx context.Context) netTestFixture {
	podNsMap := newPodNetnsCache(openNsTestOverride)
	nlDeps := &fakeIptablesDeps{}
	iptablesConfigurator := iptables.NewIptablesConfigurator(nil, &dependencies.StdoutStubDependencies{}, nlDeps)

	ztunnelServer := &fakeZtunnel{}

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPPortSet{Name: "foo", Deps: fakeIPSetDeps}
	netServer := newNetServer(ztunnelServer, podNsMap, iptablesConfigurator, NewPodNetnsProcFinder(fakeFs()), set)

	netServer.netnsRunner = func(fdable NetnsFd, toRun func() error) error {
		return toRun()
	}
	netServer.Start(ctx)
	return netTestFixture{
		netServer:            netServer,
		podNsMap:             podNsMap,
		ztunnelServer:        ztunnelServer,
		iptablesConfigurator: iptablesConfigurator,
		nlDeps:               nlDeps,
		ipsetDeps:            fakeIPSetDeps,
	}
}

func buildConvincingPod(multipleContainers bool) *corev1.Pod {
	app1 := corev1.Container{
		Name: "app1",
		Ports: []corev1.ContainerPort{
			{
				Name:          "foo-port",
				ContainerPort: 8010,
			},
			{
				Name:          "foo-2-port",
				ContainerPort: 8020,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromString("foo-2-port"),
				},
			},
		},
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(7777),
				},
			},
		},
	}
	app2 := corev1.Container{
		Name: "app2",
		Ports: []corev1.ContainerPort{
			{
				Name:          "bar-port",
				ContainerPort: 7010,
			},
			{
				Name:          "bar-2-port",
				ContainerPort: 7020,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromString("bar-port"),
				},
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				GRPC: &corev1.GRPCAction{
					Port: 6666,
				},
			},
		},
	}

	var containers []corev1.Container
	if multipleContainers {
		containers = []corev1.Container{app1, app2}
	} else {
		containers = []corev1.Container{app1}
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
			UID:       "123",
		},
		Spec: corev1.PodSpec{
			Containers: containers,
		},
		Status: corev1.PodStatus{
			PodIP:  "2.2.2.2",
			PodIPs: []corev1.PodIP{{IP: "2.2.2.2"}, {IP: "3.3.3.3"}},
		},
	}
}

func TestServerAddPod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()
	fixture := getTestFixure(ctx)
	netServer := fixture.netServer
	ztunnelServer := fixture.ztunnelServer
	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       "123",
	}
	podIP := netip.MustParseAddr("99.9.9.9")
	podIPs := []netip.Addr{podIP}
	err := netServer.AddPodToMesh(ctx, &corev1.Pod{ObjectMeta: podMeta}, podIPs, "fakenetns")
	assert.NoError(t, err)
	assert.EqualValues(t, ztunnelServer.addedPods.Load(), 1)
}

func TestServerRemovePod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()
	fixture := getTestFixure(ctx)
	netServer := fixture.netServer
	ztunnelServer := fixture.ztunnelServer
	nlDeps := fixture.nlDeps
	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       "123",
	}

	// this is usually called after add. so manually add the pod uid for now
	fakens := newFakeNs(123)
	closed := fakens.closed
	fixture.podNsMap.UpsertPodCacheWithNetns(string(podMeta.UID), fakens)
	err := netServer.RemovePodFromMesh(ctx, &corev1.Pod{ObjectMeta: podMeta})
	assert.NoError(t, err)
	assert.EqualValues(t, ztunnelServer.deletedPods.Load(), 1)
	assert.EqualValues(t, nlDeps.DelInpodMarkIPRuleCnt.Load(), 1)
	assert.EqualValues(t, nlDeps.DelLoopbackRoutesCnt.Load(), 1)
	// make sure the uid was taken from cache and netns closed
	assert.Equal(t, nil, fixture.podNsMap.Take(string(podMeta.UID)))

	// run gc to clean up ns:

	//revive:disable-next-line:call-to-gc Just a test that we are cleaning up the netns
	runtime.GC()

	assert.Equal(t, true, closed.Load())
}

func TestServerDeletePod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()
	fixture := getTestFixure(ctx)
	netServer := fixture.netServer
	ztunnelServer := fixture.ztunnelServer
	nlDeps := fixture.nlDeps
	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       "123",
	}

	// this is usually called after add. so manually add the pod uid for now
	fakens := newFakeNs(123)
	closed := fakens.closed
	fixture.podNsMap.UpsertPodCacheWithNetns(string(podMeta.UID), fakens)
	err := netServer.DelPodFromMesh(ctx, &corev1.Pod{ObjectMeta: podMeta})
	assert.NoError(t, err)
	assert.EqualValues(t, ztunnelServer.deletedPods.Load(), 1)
	// with delete iptables is not called, as there is no need to delete the iptables rules
	// from a pod that's gone from the cluster.
	assert.EqualValues(t, nlDeps.DelInpodMarkIPRuleCnt.Load(), 0)
	assert.EqualValues(t, nlDeps.DelLoopbackRoutesCnt.Load(), 0)
	// make sure the uid was taken from cache and netns closed
	assert.Equal(t, nil, fixture.podNsMap.Take(string(podMeta.UID)))
	// run gc to clean up ns:

	//revive:disable-next-line:call-to-gc Just a test that we are cleaning up the netns
	runtime.GC()
	assert.Equal(t, true, closed.Load())
}

func TestServerAddPodWithNoNetns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()
	fixture := getTestFixure(ctx)
	netServer := fixture.netServer
	ztunnelServer := fixture.ztunnelServer
	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		// this uid exists in the fake filesystem.
		UID: "863b91d4-4b68-4efa-917f-4b560e3e86aa",
	}
	podIP := netip.MustParseAddr("99.9.9.9")
	podIPs := []netip.Addr{podIP}
	err := netServer.AddPodToMesh(ctx, &corev1.Pod{ObjectMeta: podMeta}, podIPs, "")
	assert.NoError(t, err)
	assert.EqualValues(t, ztunnelServer.addedPods.Load(), 1)
}

func TestReturnsPartialErrorOnZtunnelFail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()
	fixture := getTestFixure(ctx)
	netServer := fixture.netServer
	ztunnelServer := fixture.ztunnelServer

	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       "123",
	}
	ztunnelServer.addError = errors.New("fake error")
	podIP := netip.MustParseAddr("99.9.9.9")
	podIPs := []netip.Addr{podIP}
	err := netServer.AddPodToMesh(ctx, &corev1.Pod{ObjectMeta: podMeta}, podIPs, "faksens")
	assert.EqualValues(t, ztunnelServer.addedPods.Load(), 1)
	if !errors.Is(err, ErrPartialAdd) {
		t.Fatal("expected partial error")
	}
}

func TestDoesntReturnsPartialErrorOnIptablesFail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()
	fixture := getTestFixure(ctx)
	netServer := fixture.netServer
	ztunnelServer := fixture.ztunnelServer
	nlDeps := fixture.nlDeps
	nlDeps.AddRouteErr = errors.New("fake error")

	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       "123",
	}
	podIP := netip.MustParseAddr("99.9.9.9")
	podIPs := []netip.Addr{podIP}
	err := netServer.AddPodToMesh(ctx, &corev1.Pod{ObjectMeta: podMeta}, podIPs, "faksens")
	// no calls to ztunnel if iptables failed
	assert.EqualValues(t, ztunnelServer.addedPods.Load(), 0)

	// error is not partial error
	if errors.Is(err, ErrPartialAdd) {
		t.Fatal("expected not a partial error")
	}
}

func TestConstructInitialSnap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()
	fixture := getTestFixure(ctx)
	netServer := fixture.netServer

	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
	}
	pod := &corev1.Pod{ObjectMeta: podMeta}

	fixture.ipsetDeps.On("listEntriesByIP",
		"foo",
	).Return([]netip.Addr{}, nil)

	err := netServer.ConstructInitialSnapshot([]*corev1.Pod{pod})
	assert.NoError(t, err)
	if fixture.podNsMap.Get("863b91d4-4b68-4efa-917f-4b560e3e86aa") == nil {
		t.Fatal("expected pod to be in cache")
	}
}

func TestGetPodProbePorts(t *testing.T) {
	pod := buildConvincingPod(true)
	probePorts := getPodProbePorts(pod)
	assert.Equal(t, len(probePorts), 4)

	assert.Equal(t, probePorts.Contains(8020), true)
	assert.Equal(t, probePorts.Contains(7777), true)
	assert.Equal(t, probePorts.Contains(7010), true)
	assert.Equal(t, probePorts.Contains(6666), true)
	assert.Equal(t, probePorts.Contains(7020), false)
	assert.Equal(t, probePorts.Contains(8010), false)
}

func TestAddPodProbePortsToHostNSIPSets(t *testing.T) {
	pod := buildConvincingPod(false)

	var podUID string = string(pod.ObjectMeta.UID)
	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPPortSet{Name: "foo", Deps: fakeIPSetDeps}
	ipProto := uint8(unix.IPPROTO_TCP)

	fakeIPSetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("99.9.9.9"),
		uint16(8020),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fakeIPSetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("99.9.9.9"),
		uint16(7777),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fakeIPSetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("2.2.2.2"),
		uint16(8020),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fakeIPSetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("2.2.2.2"),
		uint16(7777),
		ipProto,
		podUID,
		true,
	).Return(nil)

	podIPs := []netip.Addr{netip.MustParseAddr("99.9.9.9"), netip.MustParseAddr("2.2.2.2")}
	probePorts, err := addPodProbePortsToHostNSIpset(pod, podIPs, &set)
	assert.NoError(t, err)

	fakeIPSetDeps.AssertExpectations(t)
	assert.Equal(t, len(probePorts), 2)

	assert.Equal(t, probePorts.Contains(8020), true)
	assert.Equal(t, probePorts.Contains(7777), true)
}

func TestAddPodProbePortsToHostNSIPSetsReturnsErrorIfOneFails(t *testing.T) {
	pod := buildConvincingPod(false)

	var podUID string = string(pod.ObjectMeta.UID)
	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPPortSet{Name: "foo", Deps: fakeIPSetDeps}
	ipProto := uint8(unix.IPPROTO_TCP)

	fakeIPSetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("99.9.9.9"),
		uint16(8020),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fakeIPSetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("99.9.9.9"),
		uint16(7777),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fakeIPSetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("2.2.2.2"),
		uint16(8020),
		ipProto,
		podUID,
		true,
	).Return(errors.New("bwoah"))

	fakeIPSetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("2.2.2.2"),
		uint16(7777),
		ipProto,
		podUID,
		true,
	).Return(nil)

	podIPs := []netip.Addr{netip.MustParseAddr("99.9.9.9"), netip.MustParseAddr("2.2.2.2")}
	probePorts, err := addPodProbePortsToHostNSIpset(pod, podIPs, &set)
	assert.Error(t, err)

	fakeIPSetDeps.AssertExpectations(t)
	assert.Equal(t, len(probePorts), 2)

	assert.Equal(t, probePorts.Contains(8020), true)
	assert.Equal(t, probePorts.Contains(7777), true)
}

func TestRemovePodProbePortsFromHostNSIPSets(t *testing.T) {
	pod := buildConvincingPod(false)

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPPortSet{Name: "foo", Deps: fakeIPSetDeps}

	fakeIPSetDeps.On("clearEntriesWithIP",
		"foo",
		netip.MustParseAddr("3.3.3.3"),
	).Return(nil)

	fakeIPSetDeps.On("clearEntriesWithIP",
		"foo",
		netip.MustParseAddr("2.2.2.2"),
	).Return(nil)

	err := removePodProbePortsFromHostNSIpset(pod, &set)
	assert.NoError(t, err)
	fakeIPSetDeps.AssertExpectations(t)
}

func TestSyncHostIPSetsPrunesNothingIfNoExtras(t *testing.T) {
	pod := buildConvincingPod(false)

	fakeIPSetDeps := ipset.FakeNLDeps()

	var podUID string = string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)
	ctx, cancel := context.WithCancel(context.Background())
	fixture := getTestFixure(ctx)
	defer cancel()
	setupLogging()

	// expectations
	fixture.ipsetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("3.3.3.3"),
		uint16(8020),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fixture.ipsetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("3.3.3.3"),
		uint16(7777),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fixture.ipsetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("2.2.2.2"),
		uint16(8020),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fixture.ipsetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("2.2.2.2"),
		uint16(7777),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fixture.ipsetDeps.On("listEntriesByIP",
		"foo",
	).Return([]netip.Addr{}, nil)

	netServer := fixture.netServer
	err := netServer.syncHostIPSets([]*corev1.Pod{pod})
	assert.NoError(t, err)
	fakeIPSetDeps.AssertExpectations(t)
}

func TestSyncHostIPSetsAddsNothingIfPodHasNoIPs(t *testing.T) {
	pod := buildConvincingPod(false)

	pod.Status.PodIP = ""
	pod.Status.PodIPs = []corev1.PodIP{}

	fakeIPSetDeps := ipset.FakeNLDeps()

	ctx, cancel := context.WithCancel(context.Background())
	fixture := getTestFixure(ctx)
	defer cancel()
	setupLogging()

	fixture.ipsetDeps.On("listEntriesByIP",
		"foo",
	).Return([]netip.Addr{}, nil)

	netServer := fixture.netServer
	err := netServer.syncHostIPSets([]*corev1.Pod{pod})
	assert.NoError(t, err)
	fakeIPSetDeps.AssertExpectations(t)
}

func TestSyncHostIPSetsPrunesIfExtras(t *testing.T) {
	pod := buildConvincingPod(false)

	fakeIPSetDeps := ipset.FakeNLDeps()

	var podUID string = string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)
	ctx, cancel := context.WithCancel(context.Background())
	fixture := getTestFixure(ctx)
	defer cancel()
	setupLogging()

	// expectations
	fixture.ipsetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("3.3.3.3"),
		uint16(8020),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fixture.ipsetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("3.3.3.3"),
		uint16(7777),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fixture.ipsetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("2.2.2.2"),
		uint16(8020),
		ipProto,
		podUID,
		true,
	).Return(nil)

	fixture.ipsetDeps.On("addIPPort",
		"foo",
		netip.MustParseAddr("2.2.2.2"),
		uint16(7777),
		ipProto,
		podUID,
		true,
	).Return(nil)

	// List should return one IP not in our "pod snapshot", which means we prune
	fixture.ipsetDeps.On("listEntriesByIP",
		"foo",
	).Return([]netip.Addr{
		netip.MustParseAddr("2.2.2.2"),
		netip.MustParseAddr("6.6.6.6"),
		netip.MustParseAddr("3.3.3.3"),
	}, nil)

	fixture.ipsetDeps.On("clearEntriesWithIP",
		"foo",
		netip.MustParseAddr("6.6.6.6"),
	).Return(nil)

	netServer := fixture.netServer
	err := netServer.syncHostIPSets([]*corev1.Pod{pod})
	assert.NoError(t, err)
	fakeIPSetDeps.AssertExpectations(t)
}
