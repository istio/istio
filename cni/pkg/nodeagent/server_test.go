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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/cni/pkg/ipset"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/util/assert"
)

func TestMeshDataplaneAddsAnnotationOnAdd(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
			UID:       types.UID("test"),
		},
	}

	fakeCtx := context.Background()
	fakeClientSet := fake.NewClientset(pod)

	podIP := netip.MustParseAddr("99.9.9.1")
	podIPs := []netip.Addr{podIP}

	server := &fakeServer{}
	server.On("AddPodToMesh",
		fakeCtx,
		pod,
		podIPs,
		"",
	).Return(nil)

	server.Start(fakeCtx)

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}

	m := getFakeDPWithIPSet(server, fakeClientSet, set)
	expectPodAddedToIPSet(fakeIPSetDeps, podIP, pod.ObjectMeta)

	err := m.AddPodToMesh(fakeCtx, pod, podIPs, "")
	assert.NoError(t, err)

	fakeIPSetDeps.AssertExpectations(t)

	pod, err = fakeClientSet.CoreV1().Pods("test").Get(fakeCtx, "test", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, len(pod.Annotations), 1)
	assert.Equal(t, pod.Annotations[constants.AmbientRedirection], constants.AmbientRedirectionEnabled)
}

func TestMeshDataplaneAddsAnnotationOnAddWithPartialError(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
			UID:       types.UID("test"),
		},
	}
	server := &fakeServer{}

	podIP := netip.MustParseAddr("99.9.9.1")
	podIPs := []netip.Addr{podIP}
	fakeCtx := context.Background()

	server.On("AddPodToMesh",
		fakeCtx,
		pod,
		podIPs,
		"",
	).Return(ErrPartialAdd)

	server.Start(fakeCtx)
	fakeClientSet := fake.NewClientset(pod)

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}

	m := getFakeDPWithIPSet(server, fakeClientSet, set)

	err := m.AddPodToMesh(fakeCtx, pod, podIPs, "")
	assert.Error(t, err)

	// as this is a partial add error we should NOT have added to the ipset
	fakeIPSetDeps.AssertExpectations(t)

	pod, err = fakeClientSet.CoreV1().Pods("test").Get(fakeCtx, "test", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, len(pod.Annotations), 1)
	assert.Equal(t, pod.Annotations[constants.AmbientRedirection], constants.AmbientRedirectionEnabled)
}

func TestMeshDataplaneDoesntAnnotateOnAddWithRealError(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
			UID:       types.UID("test"),
		},
	}
	server := &fakeServer{}

	podIP := netip.MustParseAddr("99.9.9.1")
	podIPs := []netip.Addr{podIP}
	fakeCtx := context.Background()

	server.On("AddPodToMesh",
		fakeCtx,
		pod,
		podIPs,
		"",
	).Return(errors.New("not partial error"))

	server.Start(fakeCtx)
	fakeClientSet := fake.NewClientset(pod)

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}

	m := getFakeDPWithIPSet(server, fakeClientSet, set)

	err := m.AddPodToMesh(fakeCtx, pod, podIPs, "")
	assert.Error(t, err)

	// as this is a partial add error we should NOT have added to the ipset
	fakeIPSetDeps.AssertExpectations(t)

	pod, err = fakeClientSet.CoreV1().Pods("test").Get(fakeCtx, "test", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, len(pod.Annotations), 0)
}

func TestMeshDataplaneRemovePodRemovesAnnotation(t *testing.T) {
	pod := podWithAnnotation()
	fakeCtx := context.Background()

	server := &fakeServer{}
	server.Start(fakeCtx)

	server.On("RemovePodFromMesh",
		fakeCtx,
		pod,
		false,
	).Return(nil)

	fakeClientSet := fake.NewClientset(pod)

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}

	m := getFakeDPWithIPSet(server, fakeClientSet, set)
	expectPodRemovedFromIPSet(fakeIPSetDeps, pod.Status.PodIPs)

	err := m.RemovePodFromMesh(fakeCtx, pod, false)
	assert.NoError(t, err)

	fakeIPSetDeps.AssertExpectations(t)

	pod, err = fakeClientSet.CoreV1().Pods("test").Get(fakeCtx, "test", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, len(pod.Annotations), 0)
}

func TestMeshDataplaneRemovePodErrorDoesntRemoveAnnotation(t *testing.T) {
	pod := podWithAnnotation()
	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)

	server.On("RemovePodFromMesh",
		fakeCtx,
		pod,
		false,
	).Return(errors.New("fake error"))

	fakeClientSet := fake.NewClientset(pod)

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}

	m := getFakeDPWithIPSet(server, fakeClientSet, set)
	expectPodRemovedFromIPSet(fakeIPSetDeps, pod.Status.PodIPs)

	err := m.RemovePodFromMesh(fakeCtx, pod, false)
	assert.Error(t, err)

	fakeIPSetDeps.AssertExpectations(t)

	pod, err = fakeClientSet.CoreV1().Pods("test").Get(fakeCtx, "test", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, pod.Annotations[constants.AmbientRedirection], constants.AmbientRedirectionEnabled)
}

func TestMeshDataplaneDelPod(t *testing.T) {
	pod := podWithAnnotation()

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)

	server.On("RemovePodFromMesh",
		fakeCtx,
		pod,
		true,
	).Return(nil)

	fakeClientSet := fake.NewClientset()

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}
	m := getFakeDPWithIPSet(server, fakeClientSet, set)
	expectPodRemovedFromIPSet(fakeIPSetDeps, pod.Status.PodIPs)

	// pod is not in fake client, so if this will try to remove annotation, it will fail.
	err := m.RemovePodFromMesh(fakeCtx, pod, true)

	fakeIPSetDeps.AssertExpectations(t)

	assert.NoError(t, err)
}

func TestMeshDataplaneDelPodErrorDoesntPatchPod(t *testing.T) {
	pod := podWithAnnotation()

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)

	server.On("RemovePodFromMesh",
		fakeCtx,
		pod,
		true,
	).Return(errors.New("fake error"))

	fakeClientSet := fake.NewClientset()

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}

	m := getFakeDPWithIPSet(server, fakeClientSet, set)
	expectPodRemovedFromIPSet(fakeIPSetDeps, pod.Status.PodIPs)

	// pod is not in fake client, so if this will try to remove annotation, it will fail.
	err := m.RemovePodFromMesh(fakeCtx, pod, true)

	fakeIPSetDeps.AssertExpectations(t)

	assert.Error(t, err)
}

func TestMeshDataplaneAddPodToHostNSIPSets(t *testing.T) {
	pod := buildConvincingPod(false)

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)
	fakeClientSet := fake.NewClientset()

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}
	m := getFakeDPWithIPSet(server, fakeClientSet, set)

	var podUID string = string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)

	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("99.9.9.9"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("2.2.2.2"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	podIPs := []netip.Addr{netip.MustParseAddr("99.9.9.9"), netip.MustParseAddr("2.2.2.2")}
	_, err := m.addPodToHostNSIpset(pod, podIPs)
	assert.NoError(t, err)

	fakeIPSetDeps.AssertExpectations(t)
}

func TestMeshDataplaneAddPodToHostNSIPSetsV6(t *testing.T) {
	pod := buildConvincingPod(true)

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)
	fakeClientSet := fake.NewClientset()

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", V6Name: "foo-v6", Prefix: "foo", Deps: fakeIPSetDeps}
	m := getFakeDPWithIPSet(server, fakeClientSet, set)

	var podUID string = string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)

	fakeIPSetDeps.On("addIP",
		"foo-v6",
		netip.MustParseAddr("e9ac:1e77:90ca:399f:4d6d:ece2:2f9b:3164"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	fakeIPSetDeps.On("addIP",
		"foo-v6",
		netip.MustParseAddr("e9ac:1e77:90ca:399f:4d6d:ece2:2f9b:3165"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	podIPs := []netip.Addr{netip.MustParseAddr(pod.Status.PodIPs[0].IP), netip.MustParseAddr(pod.Status.PodIPs[1].IP)}
	_, err := m.addPodToHostNSIpset(pod, podIPs)
	assert.NoError(t, err)

	fakeIPSetDeps.AssertExpectations(t)
}

func TestMeshDataplaneAddPodToHostNSIPSetsDualstack(t *testing.T) {
	pod := buildConvincingPod(false)

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)
	fakeClientSet := fake.NewClientset()

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", V6Name: "foo-v6", Prefix: "foo", Deps: fakeIPSetDeps}
	m := getFakeDPWithIPSet(server, fakeClientSet, set)

	var podUID string = string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)

	fakeIPSetDeps.On("addIP",
		"foo-v6",
		netip.MustParseAddr("e9ac:1e77:90ca:399f:4d6d:ece3:2f9b:3162"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("99.9.9.9"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	podIPs := []netip.Addr{netip.MustParseAddr("e9ac:1e77:90ca:399f:4d6d:ece3:2f9b:3162"), netip.MustParseAddr("99.9.9.9")}
	_, err := m.addPodToHostNSIpset(pod, podIPs)
	assert.NoError(t, err)

	fakeIPSetDeps.AssertExpectations(t)
}

func TestMeshDataplaneAddPodIPToHostNSIPSetsReturnsErrorIfOneFails(t *testing.T) {
	pod := buildConvincingPod(false)

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)
	fakeClientSet := fake.NewClientset()

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}
	m := getFakeDPWithIPSet(server, fakeClientSet, set)

	var podUID string = string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)

	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("99.9.9.9"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("2.2.2.2"),
		ipProto,
		podUID,
		false,
	).Return(errors.New("bwoah"))

	podIPs := []netip.Addr{netip.MustParseAddr("99.9.9.9"), netip.MustParseAddr("2.2.2.2")}
	addedPIPs, err := m.addPodToHostNSIpset(pod, podIPs)
	assert.Error(t, err)
	assert.Equal(t, 1, len(addedPIPs), "only expected one IP to be added")

	fakeIPSetDeps.AssertExpectations(t)
}

func TestMeshDataplaneRemovePodIPFromHostNSIPSets(t *testing.T) {
	pod := buildConvincingPod(false)

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}

	fakeIPSetDeps.On("clearEntriesWithIP",
		"foo-v4",
		netip.MustParseAddr("3.3.3.3"),
	).Return(nil)

	fakeIPSetDeps.On("clearEntriesWithIP",
		"foo-v4",
		netip.MustParseAddr("2.2.2.2"),
	).Return(nil)

	err := removePodFromHostNSIpset(pod, &set)
	assert.NoError(t, err)
	fakeIPSetDeps.AssertExpectations(t)
}

func TestMeshDataplaneSyncHostIPSetsPrunesNothingIfNoExtras(t *testing.T) {
	pod := buildConvincingPod(false)

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)
	fakeClientSet := fake.NewClientset()

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}
	m := getFakeDPWithIPSet(server, fakeClientSet, set)

	var podUID string = string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)

	// expectations
	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("3.3.3.3"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("2.2.2.2"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	fakeIPSetDeps.On("listEntriesByIP",
		"foo-v4",
	).Return([]netip.Addr{}, nil)

	err := m.syncHostIPSets([]*corev1.Pod{pod})
	assert.NoError(t, err)
	fakeIPSetDeps.AssertExpectations(t)
}

func TestMeshDataplaneSyncHostIPSetsIgnoresPodIPAddErrorAndContinues(t *testing.T) {
	pod1 := buildConvincingPod(false)
	pod2 := buildConvincingPod(false)

	pod2.ObjectMeta.SetUID("4455")

	fakeClientSet := fake.NewClientset()

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}
	m := getFakeDPWithIPSet(server, fakeClientSet, set)

	var pod1UID string = string(pod1.ObjectMeta.UID)
	var pod2UID string = string(pod2.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)

	// First IP of first pod should error, but we should add the rest
	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("3.3.3.3"),
		ipProto,
		pod1UID,
		false,
	).Return(errors.New("CANNOT ADD"))

	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("2.2.2.2"),
		ipProto,
		pod1UID,
		false,
	).Return(nil)

	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("3.3.3.3"),
		ipProto,
		pod2UID,
		false,
	).Return(errors.New("CANNOT ADD"))

	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("2.2.2.2"),
		ipProto,
		pod2UID,
		false,
	).Return(nil)

	fakeIPSetDeps.On("listEntriesByIP",
		"foo-v4",
	).Return([]netip.Addr{}, nil)

	err := m.syncHostIPSets([]*corev1.Pod{pod1, pod2})
	assert.NoError(t, err)
	fakeIPSetDeps.AssertExpectations(t)
}

func TestMeshDataplaneSyncHostIPSetsAddsNothingIfPodHasNoIPs(t *testing.T) {
	pod := buildConvincingPod(false)

	pod.Status.PodIP = ""
	pod.Status.PodIPs = []corev1.PodIP{}

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)
	fakeClientSet := fake.NewClientset()

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}
	m := getFakeDPWithIPSet(server, fakeClientSet, set)

	fakeIPSetDeps.On("listEntriesByIP",
		"foo-v4",
	).Return([]netip.Addr{}, nil)

	err := m.syncHostIPSets([]*corev1.Pod{pod})
	assert.NoError(t, err)
	fakeIPSetDeps.AssertExpectations(t)
}

func TestMeshDataplaneSyncHostIPSetsPrunesIfExtras(t *testing.T) {
	pod := buildConvincingPod(false)

	var podUID string = string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)
	fakeClientSet := fake.NewClientset()

	fakeIPSetDeps := ipset.FakeNLDeps()
	set := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}
	m := getFakeDPWithIPSet(server, fakeClientSet, set)

	// expectations
	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("3.3.3.3"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	fakeIPSetDeps.On("addIP",
		"foo-v4",
		netip.MustParseAddr("2.2.2.2"),
		ipProto,
		podUID,
		false,
	).Return(nil)

	// List should return one IP not in our "pod snapshot", which means we prune
	fakeIPSetDeps.On("listEntriesByIP",
		"foo-v4",
	).Return([]netip.Addr{
		netip.MustParseAddr("2.2.2.2"),
		netip.MustParseAddr("6.6.6.6"),
		netip.MustParseAddr("3.3.3.3"),
	}, nil)

	fakeIPSetDeps.On("clearEntriesWithIP",
		"foo-v4",
		netip.MustParseAddr("6.6.6.6"),
	).Return(nil)

	err := m.syncHostIPSets([]*corev1.Pod{pod})
	assert.NoError(t, err)
	fakeIPSetDeps.AssertExpectations(t)
}

func podWithAnnotation() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
			UID:       types.UID("test"),
			Annotations: map[string]string{
				constants.AmbientRedirection: constants.AmbientRedirectionEnabled,
			},
		},
	}
}

type fakeServer struct {
	mock.Mock
	testWG *WaitGroup // optional waitgroup, if code under test makes a number of async calls to fakeServer
}

func (f *fakeServer) AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error {
	if f.testWG != nil {
		defer f.testWG.Done()
	}
	args := f.Called(ctx, pod, podIPs, netNs)
	return args.Error(0)
}

func (f *fakeServer) RemovePodFromMesh(ctx context.Context, pod *corev1.Pod, isDelete bool) error {
	if f.testWG != nil {
		defer f.testWG.Done()
	}
	args := f.Called(ctx, pod, isDelete)
	return args.Error(0)
}

func (f *fakeServer) Start(ctx context.Context) {
}

func (f *fakeServer) Stop() {
}

func (f *fakeServer) ConstructInitialSnapshot(ambientPods []*corev1.Pod) error {
	if f.testWG != nil {
		defer f.testWG.Done()
	}
	args := f.Called(ambientPods)
	return args.Error(0)
}

// Custom "wait group with timeout" for waiting for fakeServer calls in a goroutine to finish
type WaitGroup struct {
	count int32
	done  chan struct{}
}

func NewWaitGroup() *WaitGroup {
	return &WaitGroup{
		done: make(chan struct{}),
	}
}

func NewWaitForNCalls(t *testing.T, n int32) (*WaitGroup, func()) {
	wg := &WaitGroup{
		done: make(chan struct{}),
	}

	wg.Add(n)
	return wg, func() {
		select {
		case <-wg.C():
			return
		case <-time.After(time.Second):
			t.Fatal("Wait group timed out!\n")
		}
	}
}

func (wg *WaitGroup) Add(i int32) {
	select {
	case <-wg.done:
		panic("use of an already closed WaitGroup")
	default:
	}
	atomic.AddInt32(&wg.count, i)
}

func (wg *WaitGroup) Done() {
	i := atomic.AddInt32(&wg.count, -1)
	if i == 0 {
		close(wg.done)
	}
}

func (wg *WaitGroup) C() <-chan struct{} {
	return wg.done
}

func expectPodAddedToIPSet(ipsetDeps *ipset.MockedIpsetDeps, podIP netip.Addr, podMeta metav1.ObjectMeta) {
	ipsetDeps.On("addIP",
		"foo-v4",
		podIP,
		uint8(unix.IPPROTO_TCP),
		string(podMeta.UID),
		false,
	).Return(nil)
}

func expectPodRemovedFromIPSet(ipsetDeps *ipset.MockedIpsetDeps, podIPs []corev1.PodIP) {
	for _, ip := range podIPs {
		ipsetDeps.On("clearEntriesWithIP",
			"foo-v4",
			netip.MustParseAddr(ip.IP),
		).Return(nil)
	}
}

func getFakeDPWithIPSet(fs *fakeServer, fakeClient kubernetes.Interface, fakeSet ipset.IPSet) *meshDataplane {
	return &meshDataplane{
		kubeClient:         fakeClient,
		netServer:          fs,
		hostsideProbeIPSet: fakeSet,
	}
}

func getFakeDP(fs *fakeServer, fakeClient kubernetes.Interface) *meshDataplane {
	fakeIPSetDeps := ipset.FakeNLDeps()

	fakeIPSetDeps.On("addIP",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
	).Return(nil).Maybe()

	fakeIPSetDeps.On("clearEntriesWithIP", mock.Anything, mock.Anything).Return(nil).Maybe()
	fakeSet := ipset.IPSet{V4Name: "foo-v4", Prefix: "foo", Deps: fakeIPSetDeps}

	return getFakeDPWithIPSet(fs, fakeClient, fakeSet)
}
