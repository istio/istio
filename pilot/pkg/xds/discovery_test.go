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

package xds

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	uatomic "go.uber.org/atomic"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/test/util/retry"
)

func createProxies(n int) []*Connection {
	proxies := make([]*Connection, 0, n)
	for p := 0; p < n; p++ {
		proxies = append(proxies, &Connection{
			conID:       fmt.Sprintf("proxy-%v", p),
			pushChannel: make(chan *Event),
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
	defer close(stopCh)

	semaphore := make(chan struct{}, 2)
	queue := NewPushQueue()
	defer queue.ShutDown()

	proxies := createProxies(5)

	pushes := make(map[string]int)
	pushesMu := &sync.Mutex{}

	for _, proxy := range proxies {
		proxy := proxy
		// Start receive thread
		go func() {
			for {
				select {
				case p := <-proxy.pushChannel:
					p.done()
					pushesMu.Lock()
					pushes[proxy.conID]++
					pushesMu.Unlock()
				case <-stopCh:
					return
				}
			}
		}()
	}
	go doSendPushes(stopCh, semaphore, queue)

	for push := 0; push < 100; push++ {
		for _, proxy := range proxies {
			queue.Enqueue(proxy, &model.PushRequest{Push: &model.PushContext{}})
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
	defer close(stopCh)

	semaphore := make(chan struct{}, 2)
	queue := NewPushQueue()
	defer queue.ShutDown()

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
				select {
				case p := <-proxy.pushChannel:
					p.done()
					pushesMu.Lock()
					pushes[proxy.conID]++
					pushesMu.Unlock()
					wg.Done()
				case <-stopCh:
					return
				}
			}
		}()
	}
	go doSendPushes(stopCh, semaphore, queue)

	for _, proxy := range proxies {
		queue.Enqueue(proxy, &model.PushRequest{Push: &model.PushContext{}})
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

func (h *fakeStream) Send(*discovery.DiscoveryResponse) error {
	return nil
}

func (h *fakeStream) Recv() (*discovery.DiscoveryRequest, error) {
	return nil, nil
}

func (h *fakeStream) Context() context.Context {
	return context.Background()
}

func TestDebounce(t *testing.T) {
	// This test tests the timeout and debouncing of config updates
	// If it is flaking, DebounceAfter may need to be increased, or the code refactored to mock time.
	// For now, this seems to work well
	opts := debounceOptions{
		debounceAfter:     time.Millisecond * 50,
		debounceMax:       time.Millisecond * 100,
		enableEDSDebounce: false,
	}

	tests := []struct {
		name string
		test func(updateCh chan *model.PushRequest, expect func(partial, full int32))
	}{
		{
			name: "Should not debounce partial pushes",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				updateCh <- &model.PushRequest{Full: false}
				expect(1, 0)
				updateCh <- &model.PushRequest{Full: false}
				expect(2, 0)
				updateCh <- &model.PushRequest{Full: false}
				expect(3, 0)
				updateCh <- &model.PushRequest{Full: false}
				expect(4, 0)
				updateCh <- &model.PushRequest{Full: false}
				expect(5, 0)
			},
		},
		{
			name: "Should debounce full pushes",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				updateCh <- &model.PushRequest{Full: true}
				expect(0, 0)
			},
		},
		{
			name: "Should send full updates in batches",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				updateCh <- &model.PushRequest{Full: true}
				updateCh <- &model.PushRequest{Full: true}
				expect(0, 1)
			},
		},
		{
			name: "Should send full updates in batches, partial updates immediately",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				updateCh <- &model.PushRequest{Full: true}
				updateCh <- &model.PushRequest{Full: true}
				updateCh <- &model.PushRequest{Full: false}
				updateCh <- &model.PushRequest{Full: false}
				expect(2, 1)
				updateCh <- &model.PushRequest{Full: false}
				expect(3, 1)
			},
		},
		{
			name: "Should force a push after DebounceMax",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				// Send many requests within debounce window
				updateCh <- &model.PushRequest{Full: true}
				time.Sleep(opts.debounceAfter / 2)
				updateCh <- &model.PushRequest{Full: true}
				time.Sleep(opts.debounceAfter / 2)
				updateCh <- &model.PushRequest{Full: true}
				time.Sleep(opts.debounceAfter / 2)
				updateCh <- &model.PushRequest{Full: true}
				time.Sleep(opts.debounceAfter / 2)
				expect(0, 1)
			},
		},
		{
			name: "Should push synchronously after debounce",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				updateCh <- &model.PushRequest{Full: true}
				time.Sleep(opts.debounceAfter + 10*time.Millisecond)
				updateCh <- &model.PushRequest{Full: true}
				expect(0, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			updateCh := make(chan *model.PushRequest)
			pushingCh := make(chan struct{}, 1)
			errCh := make(chan error, 1)

			var partialPushes int32
			var fullPushes int32

			wg := sync.WaitGroup{}

			fakePush := func(req *model.PushRequest) {
				if req.Full {
					select {
					case pushingCh <- struct{}{}:
					default:
						errCh <- fmt.Errorf("multiple pushes happen simultaneously")
						return
					}
					atomic.AddInt32(&fullPushes, 1)
					time.Sleep(opts.debounceMax * 2)
					<-pushingCh
				} else {
					atomic.AddInt32(&partialPushes, 1)
				}
			}
			updateSent := uatomic.NewInt64(0)

			wg.Add(1)
			go func() {
				debounce(updateCh, stopCh, opts, fakePush, updateSent)
				wg.Done()
			}()

			expect := func(expectedPartial, expectedFull int32) {
				t.Helper()
				err := retry.UntilSuccess(func() error {
					select {
					case err := <-errCh:
						t.Error(err)
						return err
					default:
						partial := atomic.LoadInt32(&partialPushes)
						full := atomic.LoadInt32(&fullPushes)
						if partial != expectedPartial || full != expectedFull {
							return fmt.Errorf("got %v full and %v partial, expected %v full and %v partial", full, partial, expectedFull, expectedPartial)
						}
						return nil
					}
				}, retry.Timeout(opts.debounceAfter*8), retry.Delay(opts.debounceAfter/2))
				if err != nil {
					t.Error(err)
				}
			}

			// Send updates
			tt.test(updateCh, expect)

			close(stopCh)
			wg.Wait()
		})
	}
}

func TestShouldRespond(t *testing.T) {
	tests := []struct {
		name       string
		connection *Connection
		request    *discovery.DiscoveryRequest
		response   bool
	}{
		{
			name: "initial request",
			connection: &Connection{
				proxy: &model.Proxy{
					WatchedResources: map[string]*model.WatchedResource{},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl: v3.ClusterType,
			},
			response: true,
		},
		{
			name: "ack",
			connection: &Connection{
				proxy: &model.Proxy{
					WatchedResources: map[string]*model.WatchedResource{
						v3.ClusterType: {
							VersionSent: "v1",
							NonceSent:   "nonce",
						},
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       v3.ClusterType,
				VersionInfo:   "v1",
				ResponseNonce: "nonce",
			},
			response: false,
		},
		{
			name: "nack",
			connection: &Connection{
				proxy: &model.Proxy{
					WatchedResources: map[string]*model.WatchedResource{
						v3.ClusterType: {
							VersionSent: "v1",
							NonceSent:   "nonce",
						},
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       v3.ClusterType,
				VersionInfo:   "v1",
				ResponseNonce: "stale nonce",
			},
			response: false,
		},
		{
			name: "reconnect",
			connection: &Connection{
				proxy: &model.Proxy{
					WatchedResources: map[string]*model.WatchedResource{},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       v3.ClusterType,
				VersionInfo:   "v1",
				ResponseNonce: "reconnect nonce",
			},
			response: true,
		},
		{
			name: "resources change",
			connection: &Connection{
				proxy: &model.Proxy{
					WatchedResources: map[string]*model.WatchedResource{
						v3.EndpointType: {
							VersionSent:   "v1",
							NonceSent:     "nonce",
							ResourceNames: []string{"cluster1"},
						},
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       v3.EndpointType,
				VersionInfo:   "v1",
				ResponseNonce: "nonce",
				ResourceNames: []string{"cluster1", "cluster2"},
			},
			response: true,
		},
		{
			name: "ack with same resources",
			connection: &Connection{
				proxy: &model.Proxy{
					WatchedResources: map[string]*model.WatchedResource{
						v3.EndpointType: {
							VersionSent:   "v1",
							NonceSent:     "nonce",
							ResourceNames: []string{"cluster2", "cluster1"},
						},
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       v3.EndpointType,
				VersionInfo:   "v1",
				ResponseNonce: "nonce",
				ResourceNames: []string{"cluster1", "cluster2"},
			},
			response: false,
		},
		{
			name: "double nonce",
			connection: &Connection{
				proxy: &model.Proxy{
					WatchedResources: map[string]*model.WatchedResource{
						v3.EndpointType: {
							VersionSent:   "v1",
							NonceSent:     "nonce",
							NonceAcked:    "nonce",
							ResourceNames: []string{"cluster2", "cluster1"},
						},
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       v3.EndpointType,
				VersionInfo:   "v1",
				ResponseNonce: "nonce",
				ResourceNames: []string{"cluster1", "cluster2"},
			},
			response: true,
		},
		{
			name: "unsubscribe EDS",
			connection: &Connection{
				proxy: &model.Proxy{
					WatchedResources: map[string]*model.WatchedResource{
						v3.EndpointType: {
							VersionSent:   "v1",
							NonceSent:     "nonce",
							ResourceNames: []string{"cluster2", "cluster1"},
						},
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       v3.EndpointType,
				VersionInfo:   "v1",
				ResponseNonce: "nonce",
				ResourceNames: []string{},
			},
			response: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewFakeDiscoveryServer(t, FakeOptions{})
			if response, _ := s.Discovery.shouldRespond(tt.connection, tt.request); response != tt.response {
				t.Fatalf("Unexpected value for response, expected %v, got %v", tt.response, response)
			}
			if tt.name != "reconnect" && tt.response {
				if tt.connection.proxy.WatchedResources[tt.request.TypeUrl].NonceAcked != tt.request.ResponseNonce {
					t.Fatalf("Version & Nonce not updated properly")
				}
			}
		})
	}
}
