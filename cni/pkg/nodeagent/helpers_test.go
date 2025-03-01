// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package nodeagent

import (
	"context"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"

	istiolog "istio.io/istio/pkg/log"
)

func setupLogging() {
	opts := istiolog.DefaultOptions()
	opts.SetDefaultOutputLevel(istiolog.OverrideScopeName, istiolog.DebugLevel)
	istiolog.Configure(opts)
	for _, scope := range istiolog.Scopes() {
		scope.SetOutputLevel(istiolog.DebugLevel)
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

func (f *fakeServer) Stop(_ bool) {
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
