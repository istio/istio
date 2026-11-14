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

// blockingClearCache blocks the first Clear or ClearAll, which pushAndPublish and pushCurrent make under pushMu, to
// hold a push mid-critical-section.
type blockingClearCache struct {
	model.XdsCache
	blocked atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (c *blockingClearCache) block() {
	if c.blocked.CompareAndSwap(false, true) {
		close(c.entered)
		<-c.release
	}
}

func (c *blockingClearCache) Clear(configs sets.Set[model.ConfigKey]) {
	c.block()
	c.XdsCache.Clear(configs)
}

func (c *blockingClearCache) ClearAll() {
	c.block()
	c.XdsCache.ClearAll()
}

type blockedPush struct {
	s     *DiscoveryServer
	oldPC *model.PushContext
	newPC *model.PushContext
	conns []*Connection
	done  chan struct{}

	entered <-chan struct{}
	release func()
}

// newBlockedPush returns a server with three registered connections on oldPC. The first cache clear blocks.
func newBlockedPush(t *testing.T) *blockedPush {
	t.Helper()
	env := model.NewEnvironment()
	oldPC := model.NewPushContext()
	oldPC.InitDone.Store(true)
	env.SetPushContext(oldPC)
	s := NewDiscoveryServer(env, nil, nil)
	t.Cleanup(s.Shutdown)
	cache := &blockingClearCache{XdsCache: s.Cache, entered: make(chan struct{}), release: make(chan struct{})}
	s.Cache = cache

	conns := createProxies(3)
	for i, c := range conns {
		c.proxy = &model.Proxy{
			Metadata:        &model.NodeMetadata{ClusterID: "c1"},
			IPAddresses:     []string{fmt.Sprintf("10.0.0.%d", i)},
			LastPushContext: oldPC,
		}
		c.MarkInitialized()
		s.addCon(c.ID(), c)
	}

	newPC := model.NewPushContext()
	newPC.InitDone.Store(true)
	var once sync.Once
	p := &blockedPush{
		s: s, oldPC: oldPC, newPC: newPC, conns: conns, done: make(chan struct{}), entered: cache.entered,
		release: func() { once.Do(func() { close(cache.release) }) },
	}
	// Always unblock the push, so a failing test doesn't leak its goroutine.
	t.Cleanup(p.release)
	return p
}

// startBlockedPush starts a full push that publishes newPC and stops inside its critical section, with the
// connections still on oldPC.
func startBlockedPush(t *testing.T) *blockedPush {
	t.Helper()
	p := newBlockedPush(t)
	p.blockIn(t, func() {
		p.s.pushAndPublish(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "dr", Namespace: "default"}),
		}, p.newPC)
	})
	return p
}

// blockIn runs f, which must stop in its first cache clear, and waits until it has.
func (p *blockedPush) blockIn(t *testing.T, f func()) {
	t.Helper()
	go func() {
		defer close(p.done)
		f()
	}()
	waitDone(t, p.entered)
}

// finish lets the blocked push complete.
func (p *blockedPush) finish(t *testing.T) {
	t.Helper()
	p.release()
	waitDone(t, p.done)
}

// assertWaits runs f and checks it does not complete, or queue anything, while the blocked push is in its critical
// section. A miss only weakens the check; it can't cause a false failure. Returns a channel closed when f completes.
func (p *blockedPush) assertWaits(t *testing.T, name string, f func()) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
		t.Fatalf("%s completed while a push was in its critical section; it must wait for the push to finish", name)
	case <-time.After(100 * time.Millisecond):
	}
	if n := p.s.pushQueue.Pending(); n != 0 {
		t.Fatalf("%s: %d connections were queued before the blocked push; queue order must follow push context order", name, n)
	}
	return done
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// Push triggers must not be queued ahead of an in-flight full push, otherwise a proxy still on the old
// SidecarScope would serve and cache output built from the old DestinationRule.
func TestPushTriggersWaitForFullPush(t *testing.T) {
	triggers := map[string]func(s *DiscoveryServer){
		"EDS push": func(s *DiscoveryServer) {
			s.Push(&model.PushRequest{
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: "svc", Namespace: "default"}),
			}, false)
		},
		"WDS push": func(s *DiscoveryServer) {
			s.Push(&model.PushRequest{
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Address, Name: "addr", Namespace: "default"}),
			}, false)
		},
		"ProxyUpdate":    func(s *DiscoveryServer) { s.ProxyUpdate("c1", "10.0.0.0") },
		"debug push-all": func(s *DiscoveryServer) { AdsPushAll(s) },
	}
	for name, trigger := range triggers {
		t.Run(name, func(t *testing.T) {
			p := startBlockedPush(t)
			done := p.assertWaits(t, name, func() { trigger(p.s) })
			p.finish(t)
			waitDone(t, done)
			for range p.conns {
				_, req, _ := p.s.pushQueue.Dequeue()
				if req.Push != p.newPC {
					t.Fatalf("got push context %p, want the new one %p", req.Push, p.newPC)
				}
			}
		})
	}
}

// The reverse interleaving: a forced request that already read the old push context and is stalled before
// enqueueing must keep a full push from publishing and queueing the new context ahead of it. Otherwise the stalled
// request could land after it and hand a proxy the old context once the new one is already being processed.
func TestFullPushWaitsForStalledOldContextPush(t *testing.T) {
	p := newBlockedPush(t)
	p.blockIn(t, func() {
		p.s.Push(&model.PushRequest{Forced: true, Reason: model.NewReasonStats(model.DebugTrigger)}, false)
	})
	done := p.assertWaits(t, "full push", func() {
		p.s.pushAndPublish(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "dr", Namespace: "default"}),
		}, p.newPC)
	})
	if got := p.s.globalPushContext(); got != p.oldPC {
		t.Fatalf("full push published %p while an old-context push was in flight, want %p", got, p.oldPC)
	}
	p.finish(t)
	waitDone(t, done)
	// The two requests are merged per connection, in order: the surviving request must carry the new context.
	for range p.conns {
		_, req, _ := p.s.pushQueue.Dequeue()
		if req.Push != p.newPC || !req.Forced {
			t.Fatalf("got forced=%v push context %p, want forced on the new one %p", req.Forced, req.Push, p.newPC)
		}
	}
}

// A connection registering during a full push must not start on the old context and miss the push.
func TestRegisterConnectionWaitsForFullPush(t *testing.T) {
	p := startBlockedPush(t)
	con := createProxies(1)[0]
	con.SetID("late")
	con.proxy = &model.Proxy{Metadata: &model.NodeMetadata{}}
	done := p.assertWaits(t, "registerConnection", func() { p.s.registerConnection(con) })
	p.finish(t)
	waitDone(t, done)
	if con.proxy.LastPushContext != p.newPC {
		t.Fatalf("connection started on %p, want the new push context %p", con.proxy.LastPushContext, p.newPC)
	}
}

// cacheEntry is a minimal EDS cache entry.
type cacheEntry struct{ key uint64 }

func (e cacheEntry) Type() string                         { return model.EDSType }
func (e cacheEntry) Key() any                             { return e.key }
func (e cacheEntry) DependentConfigs() []model.ConfigHash { return nil }
func (e cacheEntry) Cacheable() bool                      { return true }

// cacheWritingGenerator tries to write its output to the XDS cache the way real generators do.
type cacheWritingGenerator struct {
	cache model.XdsCache
	entry cacheEntry
	calls int
}

func (g *cacheWritingGenerator) Generate(
	_ *model.Proxy, _ *model.WatchedResource, req *model.PushRequest,
) (model.Resources, model.XdsLogDetails, error) {
	g.calls++
	res := &discovery.Resource{Name: "r"}
	g.cache.Add(g.entry, req, res)
	return model.Resources{res}, model.DefaultXdsLogDetails, nil
}

// Debug config dumps must not write to the shared XDS cache: the proxy may lag the global push context.
func TestDebugDumpDoesNotWriteCache(t *testing.T) {
	dumps := map[string]func(s *DiscoveryServer, conn *Connection){
		"getConfigDumpByResourceType": func(s *DiscoveryServer, conn *Connection) {
			s.getConfigDumpByResourceType(conn, nil, []string{v3.EndpointType})
		},
		"connectionConfigDump": func(s *DiscoveryServer, conn *Connection) {
			if _, err := s.connectionConfigDump(conn, true); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, dump := range dumps {
		t.Run(name, func(t *testing.T) {
			s := NewDiscoveryServer(model.NewEnvironment(), nil, nil)
			t.Cleanup(s.Shutdown)
			gen := &cacheWritingGenerator{cache: s.Cache, entry: cacheEntry{key: 1}}
			s.Generators[v3.EndpointType] = gen
			conn := createProxies(1)[0]
			conn.proxy = &model.Proxy{
				Metadata:         &model.NodeMetadata{},
				LastPushContext:  model.NewPushContext(),
				WatchedResources: map[string]*model.WatchedResource{v3.EndpointType: {TypeUrl: v3.EndpointType}},
			}

			// Control: the generator does write when the request carries a Start.
			_, _, _ = gen.Generate(conn.proxy, nil, &model.PushRequest{Start: time.Now()})
			if s.Cache.Get(gen.entry) == nil {
				t.Fatal("test setup: the generator did not write to the cache for a request with Start")
			}
			s.Cache.ClearAll()
			gen.calls = 0

			dump(s, conn)
			if gen.calls != 1 {
				t.Fatalf("the dump called the generator %d times, want 1", gen.calls)
			}
			if s.Cache.Get(gen.entry) != nil {
				t.Fatal("the config dump wrote to the XDS cache")
			}
		})
	}
}
