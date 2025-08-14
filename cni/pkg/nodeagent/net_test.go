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
	"fmt"
	"net/netip"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/api/annotation"
	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

type netTestFixture struct {
	netServer               *NetServer
	podNsMap                *podNetnsCache
	ztunnelServer           *fakeZtunnel
	podIptablesConfigurator *iptables.IptablesConfigurator
	nlDeps                  *fakeIptablesDeps
}

func getTestFixure(ctx context.Context) netTestFixture {
	fakeIptDeps := &dependencies.DependenciesStub{}
	return getTestFixureWithIptablesConfig(ctx, fakeIptDeps, nil, nil)
}

// nolint: lll
func getTestFixureWithIptablesConfig(ctx context.Context, fakeDeps *dependencies.DependenciesStub, hostConfig, podConfig *config.IptablesConfig) netTestFixture {
	podNsMap := newPodNetnsCache(openNsTestOverride)
	nlDeps := &fakeIptablesDeps{}
	_, podIptC, _ := iptables.NewIptablesConfigurator(hostConfig, podConfig, fakeDeps, fakeDeps, nlDeps)

	ztunnelServer := &fakeZtunnel{}

	procFinder, err := NewPodNetnsProcFinder(fakeFs(true))
	if err != nil {
		panic("couldn't create mocked procfinder")
	}

	netServer := newNetServer(ztunnelServer, podNsMap, podIptC, procFinder)

	netServer.netnsRunner = func(fdable NetnsFd, toRun func() error) error {
		return toRun()
	}
	netServer.Start(ctx)
	return netTestFixture{
		netServer:               netServer,
		podNsMap:                podNsMap,
		ztunnelServer:           ztunnelServer,
		podIptablesConfigurator: podIptC,
		nlDeps:                  nlDeps,
	}
}

func buildConvincingPod(v6IP bool) *corev1.Pod {
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

	containers := []corev1.Container{app1}

	var podStatus corev1.PodStatus
	if v6IP {
		podStatus = corev1.PodStatus{
			PodIP:  "e9ac:1e77:90ca:399f:4d6d:ece2:2f9b:3164",
			PodIPs: []corev1.PodIP{{IP: "e9ac:1e77:90ca:399f:4d6d:ece2:2f9b:3164"}, {IP: "e9ac:1e77:90ca:399f:4d6d:ece2:2f9b:3165"}},
		}
	} else {
		podStatus = corev1.PodStatus{
			PodIP:  "2.2.2.2",
			PodIPs: []corev1.PodIP{{IP: "2.2.2.2"}, {IP: "3.3.3.3"}},
		}
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
		Status: podStatus,
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
	assert.Equal(t, 1, ztunnelServer.addedPods.Load())
}

func TestServerRemovePod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()
	fixture := getTestFixure(ctx)
	netServer := fixture.netServer
	ztunnelServer := fixture.ztunnelServer
	nlDeps := fixture.nlDeps
	pod := buildConvincingPod(false)
	// this is usually called after add. so manually add the pod uid for now
	fakens := newFakeNs(123)
	closed := fakens.closed
	workload := WorkloadInfo{
		Workload: podToWorkload(pod),
		Netns:    fakens,
	}

	fixture.podNsMap.UpsertPodCacheWithNetns(string(pod.UID), workload)
	err := netServer.RemovePodFromMesh(ctx, pod, false)
	assert.NoError(t, err)
	assert.Equal(t, ztunnelServer.deletedPods.Load(), 1)
	assert.Equal(t, nlDeps.DelInpodMarkIPRuleCnt.Load(), 1)
	assert.Equal(t, nlDeps.DelLoopbackRoutesCnt.Load(), 1)
	// make sure the uid was taken from cache and netns closed
	netns := fixture.podNsMap.Take(string(pod.UID))
	assert.Equal(t, nil, netns)

	// run gc to clean up ns:

	//revive:disable-next-line:call-to-gc Just a test that we are cleaning up the netns
	runtime.GC()
	assertNSClosed(t, closed)
}

func TestServerRemovePodAlwaysRemovesIPSetEntryEvenOnFail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()
	fixture := getTestFixure(ctx)
	netServer := fixture.netServer
	ztunnelServer := fixture.ztunnelServer
	nlDeps := fixture.nlDeps
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
			UID:       "123",
		},
		Spec: corev1.PodSpec{ServiceAccountName: "sa"},
	}

	ztunnelServer.delError = errors.New("fake error")
	// this is usually called after add. so manually add the pod uid for now
	fakens := newFakeNs(123)
	closed := fakens.closed
	workload := WorkloadInfo{
		Workload: podToWorkload(pod),
		Netns:    fakens,
	}
	fixture.podNsMap.UpsertPodCacheWithNetns(string(pod.UID), workload)
	err := netServer.RemovePodFromMesh(ctx, pod, false)
	assert.Error(t, err)
	assert.Equal(t, ztunnelServer.deletedPods.Load(), 1)
	assert.Equal(t, nlDeps.DelInpodMarkIPRuleCnt.Load(), 1)
	assert.Equal(t, nlDeps.DelLoopbackRoutesCnt.Load(), 1)
	// make sure the uid was taken from cache and netns closed
	netns := fixture.podNsMap.Take(string(pod.UID))
	assert.Equal(t, nil, netns)

	// run gc to clean up ns:

	//revive:disable-next-line:call-to-gc Just a test that we are cleaning up the netns
	runtime.GC()
	assertNSClosed(t, closed)
}

func TestServerDeletePod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()
	fixture := getTestFixure(ctx)
	netServer := fixture.netServer
	ztunnelServer := fixture.ztunnelServer
	nlDeps := fixture.nlDeps
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
			UID:       "123",
		},
		Spec: corev1.PodSpec{ServiceAccountName: "sa"},
	}

	// this is usually called after add. so manually add the pod uid for now
	fakens := newFakeNs(123)
	closed := fakens.closed
	workload := WorkloadInfo{
		Workload: podToWorkload(pod),
		Netns:    fakens,
	}
	fixture.podNsMap.UpsertPodCacheWithNetns(string(pod.UID), workload)
	err := netServer.RemovePodFromMesh(ctx, pod, true)
	assert.NoError(t, err)
	assert.Equal(t, ztunnelServer.deletedPods.Load(), 1)
	// with delete iptables is not called, as there is no need to delete the iptables rules
	// from a pod that's gone from the cluster.
	assert.Equal(t, nlDeps.DelInpodMarkIPRuleCnt.Load(), 0)
	assert.Equal(t, nlDeps.DelLoopbackRoutesCnt.Load(), 0)
	// make sure the uid was taken from cache and netns closed
	netns := fixture.podNsMap.Take(string(pod.UID))
	assert.Equal(t, nil, netns)
	// run gc to clean up ns:

	//revive:disable-next-line:call-to-gc Just a test that we are cleaning up the netns
	runtime.GC()
	assertNSClosed(t, closed)
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
	assert.Equal(t, ztunnelServer.addedPods.Load(), 1)
}

func TestReturnsRetryableErrorOnZtunnelFail(t *testing.T) {
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
	assert.Equal(t, ztunnelServer.addedPods.Load(), 1)
	if errors.Is(err, ErrNonRetryableAdd) {
		t.Fatal("expected retryable error")
	}
}

func TestDoesntReturnRetryableErrorOnIptablesFail(t *testing.T) {
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
	assert.Equal(t, ztunnelServer.addedPods.Load(), 0)

	// error is not a retryable error
	if !errors.Is(err, ErrNonRetryableAdd) {
		t.Fatal("expected a nonretryable error")
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

	err := netServer.ConstructInitialSnapshot([]*corev1.Pod{pod})
	assert.NoError(t, err)
	if fixture.podNsMap.Get("863b91d4-4b68-4efa-917f-4b560e3e86aa") == nil {
		t.Fatal("expected pod to be in cache")
	}
}

func TestConstructInitialSnapReconcilesPodsIfIptConfiguratorSupportsReconciliation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()

	podCfg := config.IptablesConfig{
		Reconcile: true,
	}

	fakeDeps := &dependencies.DependenciesStub{}

	fixture := getTestFixureWithIptablesConfig(ctx, fakeDeps, &podCfg, &podCfg)
	netServer := fixture.netServer

	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
	}
	pod := &corev1.Pod{ObjectMeta: podMeta}

	err := netServer.ConstructInitialSnapshot([]*corev1.Pod{pod})
	assert.NoError(t, err)

	// If iptables Reconcile is enabled, we should have executed some iptables rule insertions
	// on this pod when constructing the snapshot (since it is a faked pod, it had none to start with)
	assert.Equal(t, (len(fakeDeps.ExecutedAll) != 0), true)

	if fixture.podNsMap.Get("863b91d4-4b68-4efa-917f-4b560e3e86aa") == nil {
		t.Fatal("expected pod to be in cache")
	}
}

func TestConstructInitialSnapDoesNotReconcilePodIfIptablesReconciliationDisabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()

	podCfg := config.IptablesConfig{
		Reconcile: false,
	}

	fakeDeps := &dependencies.DependenciesStub{}

	fixture := getTestFixureWithIptablesConfig(ctx, fakeDeps, &podCfg, &podCfg)
	netServer := fixture.netServer

	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
	}
	pod := &corev1.Pod{ObjectMeta: podMeta}

	err := netServer.ConstructInitialSnapshot([]*corev1.Pod{pod})
	assert.NoError(t, err)

	// If iptables reconciliation is 0, pod rules should not have been touched
	// (since this is a faked pod with 0 rules, it should still have 0 rules)
	assert.Equal(t, len(fakeDeps.ExecutedAll), 0)

	if fixture.podNsMap.Get("863b91d4-4b68-4efa-917f-4b560e3e86aa") == nil {
		t.Fatal("expected pod to be in cache")
	}
}

func TestReconcilePodReturnsErrorIfNoNetnsFound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()

	podCfg := config.IptablesConfig{
		Reconcile: true,
	}

	fakeDeps := &dependencies.DependenciesStub{}

	fixture := getTestFixureWithIptablesConfig(ctx, fakeDeps, &podCfg, &podCfg)
	netServer := newNetServer(fixture.ztunnelServer, fixture.podNsMap, fixture.podIptablesConfigurator, &NoOpPodNetnsProcFinder{})

	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
	}
	pod := &corev1.Pod{ObjectMeta: podMeta}

	// make sure the uid was taken from cache
	fixture.podNsMap.Take(string(pod.UID))

	if fixture.podNsMap.Get("863b91d4-4b68-4efa-917f-4b560e3e86aa") != nil {
		t.Fatal("expected pod to NOT be in cache")
	}

	err := netServer.reconcileExistingPod(pod)
	assert.Error(t, err)
}

func TestReconcilePodReturnsNoErrorIfPodReconciles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupLogging()

	podCfg := config.IptablesConfig{
		Reconcile: true,
	}

	fakeDeps := &dependencies.DependenciesStub{}

	fixture := getTestFixureWithIptablesConfig(ctx, fakeDeps, nil, &podCfg)
	netServer := fixture.netServer

	podMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "bar",
		UID:       types.UID("863b91d4-4b68-4efa-917f-4b560e3e86aa"),
	}
	pod := &corev1.Pod{ObjectMeta: podMeta}

	// Pod is NOT in cache yet, as we haven't added it.
	// But faked cache should find it via fakeProc anyway
	// (depending on tertiary test fake behaviors in tests is bad,
	// this is another reason why we should use testify/mock
	// and explicitly define all expectations for the mock in each test)
	if fixture.podNsMap.Get("863b91d4-4b68-4efa-917f-4b560e3e86aa") != nil {
		t.Fatal("expected pod to NOT be in cache")
	}

	err := netServer.reconcileExistingPod(pod)
	assert.NoError(t, err)

	// If no error, we should have executed some iptables rule insertions
	// on this pod (since it is a faked pod, it had none to start with)
	assert.Equal(t, (len(fakeDeps.ExecutedAll) != 0), true)
}

var overrideTests = map[string]struct {
	in  corev1.Pod
	out config.PodLevelOverrides
}{
	"pod dns override": {
		in: corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test",
				Namespace:   "test",
				UID:         "12345",
				Annotations: map[string]string{annotation.AmbientDnsCapture.Name: "true"},
			},
		},
		out: config.PodLevelOverrides{
			VirtualInterfaces: []string{},
			IngressMode:       false,
			DNSProxy:          config.PodDNSEnabled,
		},
	},

	"no pod dns set": {
		in: corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test",
				UID:       "12345",
			},
		},
		out: config.PodLevelOverrides{
			VirtualInterfaces: []string{},
			IngressMode:       false,
			DNSProxy:          config.PodDNSUnset,
		},
	},

	"pod dns disabled": {
		in: corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test",
				Namespace:   "test",
				UID:         "12345",
				Annotations: map[string]string{annotation.AmbientDnsCapture.Name: "false"},
			},
		},
		out: config.PodLevelOverrides{
			VirtualInterfaces: []string{},
			IngressMode:       false,
			DNSProxy:          config.PodDNSDisabled,
		},
	},

	"pod dns disabled, ingress mode enabled, two virt interfaces": {
		in: corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test",
				UID:       "12345",
				Annotations: map[string]string{
					annotation.AmbientDnsCapture.Name:               "true",
					annotation.AmbientBypassInboundCapture.Name:     "true",
					annotation.IoIstioRerouteVirtualInterfaces.Name: "en0ps1, en1ps1",
				},
			},
		},
		out: config.PodLevelOverrides{
			VirtualInterfaces: []string{"en0ps1", "en1ps1"},
			IngressMode:       true,
			DNSProxy:          config.PodDNSEnabled,
		},
	},

	"various manglings": {
		in: corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test",
				UID:       "12345",
				Annotations: map[string]string{
					annotation.AmbientDnsCapture.Name:               "tweedledum",
					annotation.AmbientBypassInboundCapture.Name:     "tweedledee",
					annotation.IoIstioRerouteVirtualInterfaces.Name: "asd^&&*$&*(#$&*(#&$*())),   ",
				},
			},
		},
		out: config.PodLevelOverrides{
			VirtualInterfaces: []string{"asd^&&*$&*(#$&*(#&$*()))"},
			IngressMode:       false,
			DNSProxy:          config.PodDNSUnset,
		},
	},
}

func TestGetPodLevelOverrides(t *testing.T) {
	for name, test := range overrideTests {
		t.Run(name, func(t *testing.T) {
			res := getPodLevelTrafficOverrides(&test.in)
			assert.Equal(t, res, test.out, fmt.Sprintf("test '%s' failed", name))
		})
	}
}

// for tests that call `runtime.GC()` - we have no control over when the GC is actually scheduled,
// and it is flake-prone to check for closure after calling it, this retries for a bit to make
// sure the netns is closed eventually.
func assertNSClosed(t *testing.T, closed *atomic.Bool) {
	for i := 0; i < 5; i++ {
		if closed.Load() {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatal("NS not closed")
}
