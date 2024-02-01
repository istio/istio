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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

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
	fakeClientSet := fake.NewSimpleClientset(pod)

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
	m := meshDataplane{
		kubeClient: fakeClientSet,
		netServer:  server,
	}

	err := m.AddPodToMesh(fakeCtx, pod, podIPs, "")
	assert.NoError(t, err)

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
	fakeClientSet := fake.NewSimpleClientset(pod)
	m := meshDataplane{
		kubeClient: fakeClientSet,
		netServer:  server,
	}

	err := m.AddPodToMesh(fakeCtx, pod, podIPs, "")
	assert.Error(t, err)

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
	fakeClientSet := fake.NewSimpleClientset(pod)
	m := meshDataplane{
		kubeClient: fakeClientSet,
		netServer:  server,
	}

	err := m.AddPodToMesh(fakeCtx, pod, podIPs, "")
	assert.Error(t, err)

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
	).Return(nil)

	fakeClientSet := fake.NewSimpleClientset(pod)
	m := meshDataplane{
		kubeClient: fakeClientSet,
		netServer:  server,
	}

	err := m.RemovePodFromMesh(fakeCtx, pod)
	assert.NoError(t, err)

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
	).Return(errors.New("fake error"))

	fakeClientSet := fake.NewSimpleClientset(pod)
	m := meshDataplane{
		kubeClient: fakeClientSet,
		netServer:  server,
	}

	err := m.RemovePodFromMesh(fakeCtx, pod)
	assert.Error(t, err)

	pod, err = fakeClientSet.CoreV1().Pods("test").Get(fakeCtx, "test", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, pod.Annotations[constants.AmbientRedirection], constants.AmbientRedirectionEnabled)
}

func TestMeshDataplaneDelPod(t *testing.T) {
	pod := podWithAnnotation()

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)

	server.On("DelPodFromMesh",
		fakeCtx,
		pod,
	).Return(nil)

	fakeClientSet := fake.NewSimpleClientset()
	m := meshDataplane{
		kubeClient: fakeClientSet,
		netServer:  server,
	}

	// pod is not in fake client, so if this will try to remove annotation, it will fail.
	err := m.DelPodFromMesh(fakeCtx, pod)
	assert.NoError(t, err)
}

func TestMeshDataplaneDelPodErrorDoesntPatchPod(t *testing.T) {
	pod := podWithAnnotation()

	fakeCtx := context.Background()
	server := &fakeServer{}
	server.Start(fakeCtx)

	server.On("DelPodFromMesh",
		fakeCtx,
		pod,
	).Return(errors.New("fake error"))

	fakeClientSet := fake.NewSimpleClientset()
	m := meshDataplane{
		kubeClient: fakeClientSet,
		netServer:  server,
	}

	// pod is not in fake client, so if this will try to remove annotation, it will fail.
	err := m.DelPodFromMesh(fakeCtx, pod)
	assert.Error(t, err)
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

func (f *fakeServer) RemovePodFromMesh(ctx context.Context, pod *corev1.Pod) error {
	if f.testWG != nil {
		defer f.testWG.Done()
	}
	args := f.Called(ctx, pod)
	return args.Error(0)
}

func (f *fakeServer) DelPodFromMesh(ctx context.Context, pod *corev1.Pod) error {
	if f.testWG != nil {
		defer f.testWG.Done()
	}
	args := f.Called(ctx, pod)
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
