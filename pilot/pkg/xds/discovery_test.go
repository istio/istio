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
	"testing/synctest"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	uatomic "go.uber.org/atomic"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

func createProxies(n int) []*Connection {
	proxies := make([]*Connection, 0, n)
	for p := 0; p < n; p++ {
		conn := newConnection("", &fakeStream{})
		conn.SetID(fmt.Sprintf("proxy-%v", p))
		proxies = append(proxies, conn)
	}
	return proxies
}

func TestProxyUpdateByIP(t *testing.T) {
	s := NewDiscoveryServer(&model.Environment{}, nil, nil)
	t.Cleanup(s.Shutdown)

	const primaryIP = "10.0.0.1"
	const secondaryIP = "2001:db8::1"
	addClient := func(id string, clusterID cluster.ID, ip string, initialized bool) *Connection {
		con := newConnection("", nil)
		con.SetID(id)
		con.proxy = &model.Proxy{
			ID:          "proxy",
			IPAddresses: []string{ip, secondaryIP},
			Metadata:    &model.NodeMetadata{ClusterID: clusterID},
		}
		s.addCon(con)
		if initialized {
			con.MarkInitialized()
		}
		return con
	}

	first := addClient("proxy-1", "cluster-a", primaryIP, true)
	reconnect := addClient("proxy-2", "cluster-a", primaryIP, false)
	otherCluster := addClient("proxy-3", "cluster-b", primaryIP, true)
	otherIP := addClient("proxy-4", "cluster-a", "10.0.0.2", true)

	assert.Equal(t, s.adsClientCount(), 4)
	assert.Equal(t, len(s.AllClients()), 4)
	assert.Equal(t, len(s.Clients()), 3)

	s.ProxyUpdate("cluster-c", primaryIP)
	s.ProxyUpdate("cluster-a", secondaryIP)
	assert.Equal(t, s.pushQueue.Pending(), 0)

	assertPush := func(clusterID cluster.ID, want *Connection) {
		t.Helper()
		s.ProxyUpdate(clusterID, primaryIP)
		assert.Equal(t, s.pushQueue.Pending(), 1)
		got, _, _ := s.pushQueue.Dequeue()
		assert.Equal(t, got.ID(), want.ID())
		s.pushQueue.MarkDone(got)
	}
	assertPush("cluster-a", first)
	assertPush("cluster-b", otherCluster)

	s.removeCon(first)
	s.ProxyUpdate("cluster-a", primaryIP)
	assert.Equal(t, s.pushQueue.Pending(), 0)
	reconnect.MarkInitialized()
	assertPush("cluster-a", reconnect)

	s.removeCon(reconnect)
	s.removeCon(otherCluster)
	assert.Equal(t, s.adsClientCount(), 1)
	assert.Equal(t, len(s.adsClients), 1)
	s.ProxyUpdate("cluster-a", primaryIP)
	assert.Equal(t, s.pushQueue.Pending(), 0)
	s.removeCon(otherIP)
	assert.Equal(t, s.adsClientCount(), 0)
	assert.Equal(t, len(s.adsClients), 0)
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

	const numPushRounds = 100
	// Each round enqueues one push per proxy; the queue may coalesce them,
	// so track how many individual events each proxy *receives*, not sends.
	var wg sync.WaitGroup
	wg.Add(len(proxies)) // at least one push per proxy is guaranteed

	pushes := make(map[string]int)
	pushesMu := &sync.Mutex{}

	for _, proxy := range proxies {
		// Start receive thread
		go func() {
			first := true
			for {
				select {
				case ev := <-proxy.PushCh():
					p := ev.(*Event)
					p.done()
					pushesMu.Lock()
					pushes[proxy.ID()]++
					pushesMu.Unlock()
					if first {
						wg.Done()
						first = false
					}
				case <-stopCh:
					return
				}
			}
		}()
	}
	go doSendPushes(stopCh, semaphore, queue)

	// Enqueue all pushes without sleeping between rounds.
	for push := 0; push < numPushRounds; push++ {
		for _, proxy := range proxies {
			queue.Enqueue(proxy, &model.PushRequest{Push: &model.PushContext{}, Forced: true})
		}
	}

	// Wait for queue to process all requests
	retry.UntilSuccessOrFail(t, func() error {
		if queue.Pending() > 0 {
			return fmt.Errorf("queue still has %d pending items", queue.Pending())
		}
		return nil
	}, retry.Timeout(5*time.Second))

	// Wait for every proxy to receive at least one push (or timeout).
	if !wgDoneOrTimeout(&wg, 5*time.Second) {
		t.Fatal("timed out waiting for proxies to receive pushes")
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
		// Start receive thread
		go func() {
			for {
				select {
				case ev := <-proxy.PushCh():
					p := ev.(*Event)
					p.done()
					pushesMu.Lock()
					pushes[proxy.ID()]++
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
		queue.Enqueue(proxy, &model.PushRequest{Push: &model.PushContext{}, Forced: true})
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
	opts := DebounceOptions{
		DebounceAfter:     time.Millisecond * 50,
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
				updateCh <- &model.PushRequest{ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "test"})}
				expect(1, 0)
				updateCh <- &model.PushRequest{ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "test"})}
				expect(2, 0)
				updateCh <- &model.PushRequest{ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "test"})}
				expect(3, 0)
				updateCh <- &model.PushRequest{ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "test"})}
				expect(4, 0)
				updateCh <- &model.PushRequest{ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "test"})}
				expect(5, 0)
			},
		},
		{
			name: "Should initialize forced endpoint pushes",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				updateCh <- &model.PushRequest{
					Forced:         true,
					ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "test"}),
				}
				expect(0, 1)
			},
		},
		{
			name: "Should debounce full pushes",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				updateCh <- &model.PushRequest{Forced: true}
				expect(0, 0)
			},
		},
		{
			name: "Should send full updates in batches",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				updateCh <- &model.PushRequest{Forced: true}
				updateCh <- &model.PushRequest{Forced: true}
				expect(0, 1)
			},
		},
		{
			name: "Should send full updates in batches, partial updates immediately",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				updateCh <- &model.PushRequest{Forced: true}
				updateCh <- &model.PushRequest{Forced: true}
				updateCh <- &model.PushRequest{ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "test"})}
				updateCh <- &model.PushRequest{ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "test"})}
				expect(2, 1)
				updateCh <- &model.PushRequest{ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "test"})}
				expect(3, 1)
			},
		},
		{
			name: "Should force a push after DebounceMax",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				// Send many requests within debounce window
				updateCh <- &model.PushRequest{Forced: true}
				time.Sleep(opts.DebounceAfter / 2)
				updateCh <- &model.PushRequest{Forced: true}
				time.Sleep(opts.DebounceAfter / 2)
				updateCh <- &model.PushRequest{Forced: true}
				time.Sleep(opts.DebounceAfter / 2)
				updateCh <- &model.PushRequest{Forced: true}
				time.Sleep(opts.DebounceAfter / 2)
				expect(0, 1)
			},
		},
		{
			name: "Should push synchronously after debounce",
			test: func(updateCh chan *model.PushRequest, expect func(partial, full int32)) {
				updateCh <- &model.PushRequest{Forced: true}
				time.Sleep(opts.DebounceAfter + 10*time.Millisecond)
				updateCh <- &model.PushRequest{Forced: true}
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

			fakePush := func(req *model.PushRequest, initializePushContext bool) {
				if initializePushContext {
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
					if !model.OnlyHasConfigsOfKind(req.ConfigsUpdated, kind.Endpoints) {
						errCh <- fmt.Errorf("push without initialization was not endpoint-only")
						return
					}
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
				}, retry.Timeout(opts.DebounceAfter*8), retry.Delay(opts.DebounceAfter/2))
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

func TestShouldDebounce(t *testing.T) {
	for _, enableEDS := range []bool{true, false} {
		for _, enableWDS := range []bool{true, false} {
			t.Run(fmt.Sprintf("EDS=%v/WDS=%v", enableEDS, enableWDS), func(t *testing.T) {
				opts := DebounceOptions{
					enableEDSDebounce: enableEDS,
					enableWDSDebounce: enableWDS,
				}
				for _, tt := range []struct {
					name         string
					kinds        []kind.Kind
					forced       bool
					wantDebounce bool
				}{
					{name: "EDS", kinds: []kind.Kind{kind.Endpoints}, wantDebounce: enableEDS},
					{name: "WDS", kinds: []kind.Kind{kind.Address}, wantDebounce: enableWDS},
					{name: "multiple addresses", kinds: []kind.Kind{kind.Address, kind.Address}, wantDebounce: enableWDS},
					{name: "forced EDS", kinds: []kind.Kind{kind.Endpoints}, forced: true, wantDebounce: true},
					{name: "forced WDS", kinds: []kind.Kind{kind.Address}, forced: true, wantDebounce: true},
					{name: "EDS and WDS", kinds: []kind.Kind{kind.Endpoints, kind.Address}, wantDebounce: enableEDS || enableWDS},
					{name: "forced EDS and WDS", kinds: []kind.Kind{kind.Endpoints, kind.Address}, forced: true, wantDebounce: true},
					{name: "EDS and config", kinds: []kind.Kind{kind.Endpoints, kind.ServiceEntry}, wantDebounce: true},
					{name: "WDS and config", kinds: []kind.Kind{kind.Address, kind.ServiceEntry}, wantDebounce: true},
					{name: "EDS and WDS and config", kinds: []kind.Kind{kind.Endpoints, kind.Address, kind.ServiceEntry}, wantDebounce: true},
					{name: "config", kinds: []kind.Kind{kind.ServiceEntry}, wantDebounce: true},
					{name: "no configs", wantDebounce: true},
				} {
					t.Run(tt.name, func(t *testing.T) {
						configs := sets.New[model.ConfigKey]()
						for i, k := range tt.kinds {
							configs.Insert(model.ConfigKey{Kind: k, Name: fmt.Sprintf("test-%d", i)})
						}
						req := &model.PushRequest{ConfigsUpdated: configs, Forced: tt.forced}
						assert.Equal(t, opts.shouldDebounce(req), tt.wantDebounce)
					})
				}
			})
		}
	}
}

func TestPushCanReusePushContext(t *testing.T) {
	env := model.NewEnvironment()
	push := model.NewPushContext()
	push.InitDone.Store(true)
	env.SetPushContext(push)

	server := NewDiscoveryServer(env, nil, nil)
	defer server.Shutdown()
	req := &model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "test"}),
	}
	server.Push(req, false)

	if got := env.PushContext(); got != push {
		t.Fatal("endpoint-only push replaced the global push context")
	}
	if req.Push != push {
		t.Fatal("endpoint-only push did not use the existing global push context")
	}
}

func BenchmarkPushRequest(b *testing.B) {
	// allTriggers contains all triggers, so we can pick one at random.
	// It is not a big issue if it falls out of sync, as we are just trying to generate test data
	allTriggers := []model.TriggerReason{
		model.EndpointUpdate,
		model.ConfigUpdate,
		model.ServiceUpdate,
		model.ProxyUpdate,
		model.GlobalUpdate,
		model.UnknownTrigger,
		model.DebugTrigger,
		model.SecretTrigger,
		model.NetworksTrigger,
		model.ProxyRequest,
		model.NamespaceUpdate,
	}
	// Number of (simulated) proxies
	proxies := 500
	// Number of (simulated) pushes merged
	pushesMerged := 10
	// Number of configs per push
	configs := 1

	for n := 0; n < b.N; n++ {
		var req *model.PushRequest
		for i := 0; i < pushesMerged; i++ {
			trigger := allTriggers[i%len(allTriggers)]
			nreq := &model.PushRequest{
				ConfigsUpdated: sets.New[model.ConfigKey](),
				Reason:         model.NewReasonStats(trigger),
				Forced:         true,
			}
			for c := 0; c < configs; c++ {
				nreq.ConfigsUpdated.Insert(model.ConfigKey{Kind: kind.ServiceEntry, Name: fmt.Sprintf("%d", c), Namespace: "default"})
			}
			req = req.Merge(nreq)
		}
		for p := 0; p < proxies; p++ {
			recordPushTriggers(req.Reason)
		}
	}
}

// Workload updates share the debounce queue with endpoint churn. Even when the
// queue never becomes quiet, the maximum debounce delay must bound their wait.
func TestDebounceWorkloadDeadline(t *testing.T) {
	for _, tt := range []struct {
		name  string
		after time.Duration
		churn bool
	}{
		{name: "continuous churn", after: 200 * time.Millisecond, churn: true},
		{name: "quiet period exceeds maximum", after: time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				stop := make(chan struct{})
				defer close(stop)
				updates := make(chan *model.PushRequest)
				pushed := make(chan *model.PushRequest, 10)
				opts := DebounceOptions{
					DebounceAfter:     tt.after,
					debounceMax:       250 * time.Millisecond,
					enableEDSDebounce: true,
					enableWDSDebounce: true,
				}
				go debounce(updates, stop, opts, func(req *model.PushRequest, initialize bool) {
					pushed <- req
				}, uatomic.NewInt64(0))
				updates <- &model.PushRequest{
					ConfigsUpdated:   sets.New(model.ConfigKey{Kind: kind.Address, Name: "pod"}),
					AddressesUpdated: sets.New("Kubernetes//Pod/default/pod"),
				}
				if tt.churn {
					go func() {
						for {
							select {
							case <-stop:
								return
							case <-time.After(150 * time.Millisecond):
							}
							select {
							case updates <- &model.PushRequest{
								ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "service"}),
							}:
							case <-stop:
								return
							}
						}
					}()
				}
				time.Sleep(opts.debounceMax)
				synctest.Wait()
				select {
				case req := <-pushed:
					assert.Equal(t, req.AddressesUpdated, sets.New("Kubernetes//Pod/default/pod"))
				default:
					t.Fatal("workload update was not pushed by the maximum debounce deadline")
				}
			})
		})
	}
}
