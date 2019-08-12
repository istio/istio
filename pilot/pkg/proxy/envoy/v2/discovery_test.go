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
	"reflect"
	"sync"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc"

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
