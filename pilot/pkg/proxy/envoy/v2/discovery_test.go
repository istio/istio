// Copyright 2019 Istio Authors
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

package v2

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
)

func mockNeedsPush(node *model.Proxy) bool {
	return true
}

func createProxies(n int) []*XdsConnection {
	proxies := make([]*XdsConnection, 0, n)
	for p := 0; p < n; p++ {
		proxies = append(proxies, &XdsConnection{
			ConID:       fmt.Sprintf("proxy-%v", p),
			pushChannel: make(chan *XdsEvent),
			stream:      &fakeStream{},
		})
	}
	return proxies
}

func wgDoneOrTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		wg.Wait()
		c <- struct{}{}
	}()
	select {
	case <-c:
		return true
	case <-time.After(timeout):
		return false
	}
}

func TestSendPushesManyPushes(t *testing.T) {
	stopCh := make(chan struct{})
	semaphore := make(chan struct{}, 2)
	queue := NewPushQueue()

	proxies := createProxies(5)

	pushes := make(map[string]int)
	pushesMu := &sync.Mutex{}

	for _, proxy := range proxies {
		proxy := proxy
		// Start receive thread
		go func() {
			for {
				p := <-proxy.pushChannel
				p.done()
				pushesMu.Lock()
				pushes[proxy.ConID]++
				pushesMu.Unlock()
			}
		}()
	}
	go doSendPushes(stopCh, semaphore, queue, mockNeedsPush)

	for push := 0; push < 100; push++ {
		for _, proxy := range proxies {
			queue.Enqueue(proxy, &PushEvent{})
		}
		time.Sleep(time.Millisecond * 10)
	}
	for queue.Pending() > 0 {
		time.Sleep(time.Millisecond)
	}
	pushesMu.Lock()
	defer pushesMu.Unlock()
	for proxy, numPushes := range pushes {
		if numPushes == 0 {
			t.Fatalf("Proxy %v had 0 pushes", proxy)
		}
	}
}

func TestSendPushesSinglePush(t *testing.T) {
	stopCh := make(chan struct{})
	semaphore := make(chan struct{}, 2)
	queue := NewPushQueue()

	proxies := createProxies(5)

	wg := &sync.WaitGroup{}
	wg.Add(5)

	pushes := make(map[string]int)
	pushesMu := &sync.Mutex{}

	for _, proxy := range proxies {
		proxy := proxy
		// Start receive thread
		go func() {
			for {
				p := <-proxy.pushChannel
				p.done()
				pushesMu.Lock()
				pushes[proxy.ConID]++
				pushesMu.Unlock()
				wg.Done()
			}
		}()
	}
	go doSendPushes(stopCh, semaphore, queue, mockNeedsPush)

	for _, proxy := range proxies {
		queue.Enqueue(proxy, &PushEvent{})
	}

	if !wgDoneOrTimeout(wg, time.Second) {
		t.Fatalf("Expected 5 pushes but got %v", len(pushes))
	}
	expected := map[string]int{
		"proxy-0": 1,
		"proxy-1": 1,
		"proxy-2": 1,
		"proxy-3": 1,
		"proxy-4": 1,
	}
	if !reflect.DeepEqual(expected, pushes) {
		t.Fatalf("Expected pushes %+v, got %+v", expected, pushes)
	}
}

type fakeStream struct {
	grpc.ServerStream
}

func (h *fakeStream) Send(*xdsapi.DiscoveryResponse) error {
	return nil
}

func (h *fakeStream) Recv() (*xdsapi.DiscoveryRequest, error) {
	return nil, nil
}

func (h *fakeStream) Context() context.Context {
	return context.Background()
}

func TestDebounce(t *testing.T) {
	// This test tests the timeout and debouncing of config updates
	// If it is flaking, DebounceAfter may need to be increased, or the code refactored to mock time.
	// For now, this seems to work well
	DebounceAfter = time.Millisecond * 25
	DebounceMax = DebounceAfter * 2
	if err := os.Setenv(features.EnableEDSDebounce.Name, "false"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Unsetenv(features.EnableEDSDebounce.Name); err != nil {
			t.Fatal(err)
		}
	}()

	tests := []struct {
		name            string
		test            func(updateCh chan *model.UpdateRequest)
		expectedFull    int32
		expectedPartial int32
	}{
		{
			name: "Should not debounce partial pushes",
			test: func(updateCh chan *model.UpdateRequest) {
				updateCh <- &model.UpdateRequest{Full: false}
				updateCh <- &model.UpdateRequest{Full: false}
				updateCh <- &model.UpdateRequest{Full: false}
				updateCh <- &model.UpdateRequest{Full: false}
				updateCh <- &model.UpdateRequest{Full: false}
			},
			expectedFull:    0,
			expectedPartial: 5,
		},
		{
			name: "Should debounce full pushes",
			test: func(updateCh chan *model.UpdateRequest) {
				updateCh <- &model.UpdateRequest{Full: true}
			},
			expectedFull:    0,
			expectedPartial: 0,
		},
		{
			name: "Should send full updates in batches",
			test: func(updateCh chan *model.UpdateRequest) {
				updateCh <- &model.UpdateRequest{Full: true}
				updateCh <- &model.UpdateRequest{Full: true}
				time.Sleep(DebounceAfter * 3 / 2)
				updateCh <- &model.UpdateRequest{Full: true}
			},
			expectedFull:    1,
			expectedPartial: 0,
		},
		{
			name: "Should send full updates in batches, partial updates immediately",
			test: func(updateCh chan *model.UpdateRequest) {
				updateCh <- &model.UpdateRequest{Full: true}
				updateCh <- &model.UpdateRequest{Full: true}
				updateCh <- &model.UpdateRequest{Full: false}
				updateCh <- &model.UpdateRequest{Full: false}
				time.Sleep(DebounceAfter * 2)
				updateCh <- &model.UpdateRequest{Full: false}
			},
			expectedFull:    1,
			expectedPartial: 3,
		},
		{
			name: "Should force a push after DebounceMax",
			test: func(updateCh chan *model.UpdateRequest) {
				// Send many requests within debounce window
				updateCh <- &model.UpdateRequest{Full: true}
				time.Sleep(DebounceAfter / 2)
				updateCh <- &model.UpdateRequest{Full: true}
				time.Sleep(DebounceAfter / 2)
				updateCh <- &model.UpdateRequest{Full: true}
				time.Sleep(DebounceAfter / 2)
				updateCh <- &model.UpdateRequest{Full: true}
				time.Sleep(DebounceAfter / 2)
				// At this point a push should be triggered, from DebounceMax

				updateCh <- &model.UpdateRequest{Full: true}
				time.Sleep(DebounceAfter / 2)
				updateCh <- &model.UpdateRequest{Full: true}
				time.Sleep(DebounceAfter / 2)
			},
			expectedFull:    1,
			expectedPartial: 0,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			updateCh := make(chan *model.UpdateRequest)

			var partialPushes int32
			var fullPushes int32

			wg := sync.WaitGroup{}

			fakePush := func(req *model.UpdateRequest) {
				wg.Add(1)
				go func() {
					if req.Full {
						atomic.AddInt32(&fullPushes, 1)
					} else {
						atomic.AddInt32(&partialPushes, 1)
					}
					wg.Done()
				}()
			}

			wg.Add(1)
			go func() {
				debounce(updateCh, stopCh, fakePush)
				wg.Done()
			}()

			// Send updates
			tt.test(updateCh)

			close(stopCh)
			wg.Wait()

			if partialPushes != tt.expectedPartial || fullPushes != tt.expectedFull {
				t.Fatalf("Got %v full and %v partial, expected %v full and %v partial", fullPushes, partialPushes, tt.expectedFull, tt.expectedPartial)
			}
		})
	}
}
