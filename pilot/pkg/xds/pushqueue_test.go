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
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// Helper function to remove an item or timeout and return nil if there are no pending pushes
func getWithTimeout(p *PushQueue) *Connection {
	done := make(chan *Connection, 1)
	go func() {
		con, _, _ := p.Dequeue()
		done <- con
	}()
	select {
	case ret := <-done:
		return ret
	case <-time.After(time.Millisecond * 500):
		return nil
	}
}

func ExpectTimeout(t *testing.T, p *PushQueue) {
	t.Helper()
	done := make(chan struct{}, 1)
	go func() {
		p.Dequeue()
		done <- struct{}{}
	}()
	select {
	case <-done:
		t.Fatal("Expected timeout")
	case <-time.After(time.Millisecond * 500):
	}
}

func ExpectDequeue(t *testing.T, p *PushQueue, expected *Connection) {
	t.Helper()
	result := make(chan *Connection, 1)
	go func() {
		con, _, _ := p.Dequeue()
		result <- con
	}()
	select {
	case got := <-result:
		if got != expected {
			t.Fatalf("Expected proxy %v, got %v", expected, got)
		}
	case <-time.After(time.Millisecond * 500):
		t.Fatal("Timed out")
	}
}

func TestProxyQueue(t *testing.T) {
	proxies := make([]*Connection, 0, 100)
	for p := 0; p < 100; p++ {
		conn := newConnection("", nil)
		conn.SetID(fmt.Sprintf("proxy-%d", p))
		proxies = append(proxies, conn)
	}

	t.Run("simple add and remove", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()
		p.Enqueue(proxies[0], &model.PushRequest{})
		p.Enqueue(proxies[1], &model.PushRequest{})

		ExpectDequeue(t, p, proxies[0])
		ExpectDequeue(t, p, proxies[1])
	})

	t.Run("remove too many", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		p.Enqueue(proxies[0], &model.PushRequest{})

		ExpectDequeue(t, p, proxies[0])
		ExpectTimeout(t, p)
	})

	t.Run("add multiple times", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		p.Enqueue(proxies[0], &model.PushRequest{})
		p.Enqueue(proxies[1], &model.PushRequest{})
		p.Enqueue(proxies[0], &model.PushRequest{})

		ExpectDequeue(t, p, proxies[0])
		ExpectDequeue(t, p, proxies[1])
		ExpectTimeout(t, p)
	})

	t.Run("add and remove and markdone", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		p.Enqueue(proxies[0], &model.PushRequest{})
		ExpectDequeue(t, p, proxies[0])
		p.MarkDone(proxies[0])
		p.Enqueue(proxies[0], &model.PushRequest{})
		ExpectDequeue(t, p, proxies[0])
		ExpectTimeout(t, p)
	})

	t.Run("add and remove and add and markdone", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		p.Enqueue(proxies[0], &model.PushRequest{})
		ExpectDequeue(t, p, proxies[0])
		p.Enqueue(proxies[0], &model.PushRequest{})
		p.Enqueue(proxies[0], &model.PushRequest{})
		p.MarkDone(proxies[0])

		ExpectDequeue(t, p, proxies[0])
		ExpectTimeout(t, p)
	})

	t.Run("remove should block", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		wg := &sync.WaitGroup{}
		wg.Add(1)
		go func() {
			ExpectDequeue(t, p, proxies[0])
			wg.Done()
		}()
		time.Sleep(time.Millisecond * 50)
		p.Enqueue(proxies[0], &model.PushRequest{})
		wg.Wait()
	})

	t.Run("should merge model.PushRequest", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		firstTime := time.Now()
		p.Enqueue(proxies[0], &model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{
				Kind: kind.ServiceEntry,
				Name: "foo",
			}),
			Start: firstTime,
		})

		p.Enqueue(proxies[0], &model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "bar", Namespace: "ns1"}),

			Start: firstTime.Add(time.Second),
		})
		_, info, _ := p.Dequeue()

		if info.Start != firstTime {
			t.Errorf("Expected start time to be %v, got %v", firstTime, info.Start)
		}
		expectedEds := sets.New(
			model.ConfigKey{
				Kind:      kind.ServiceEntry,
				Name:      "foo",
				Namespace: "",
			},
			model.ConfigKey{
				Kind:      kind.ServiceEntry,
				Name:      "bar",
				Namespace: "ns1",
			},
		)
		if !reflect.DeepEqual(model.ConfigsOfKind(info.ConfigsUpdated, kind.ServiceEntry), expectedEds) {
			t.Errorf("Expected EdsUpdates to be %v, got %v", expectedEds, model.ConfigsOfKind(info.ConfigsUpdated, kind.ServiceEntry))
		}
	})

	t.Run("merging does not mutate shared requests", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		key := func(name string) model.ConfigKey { return model.ConfigKey{Kind: kind.Endpoints, Name: name} }
		newReq := func(name string) *model.PushRequest {
			return &model.PushRequest{ConfigsUpdated: sets.New(key(name)), Reason: model.NewReasonStats(model.EndpointUpdate)}
		}
		// Requests are shared across connections, as StartPush does.
		a, b, c, d := newReq("a"), newReq("b"), newReq("c"), newReq("d")
		for _, req := range []*model.PushRequest{a, b, c} {
			p.Enqueue(proxies[0], req)
			p.Enqueue(proxies[1], req)
		}
		for _, con := range []*Connection{proxies[0], proxies[1]} {
			got, req, _ := p.Dequeue()
			if got != con {
				t.Fatalf("expected %v, got %v", con.ID(), got.ID())
			}
			if !req.ConfigsUpdated.Equals(sets.New(key("a"), key("b"), key("c"))) {
				t.Fatalf("unexpected configs %v", req.ConfigsUpdated)
			}
			if req.Reason.Count() != 3 {
				t.Fatalf("unexpected reasons %v", req.Reason)
			}
		}

		// Shared inputs must stay unchanged during processing and after MarkDone requeues updates.
		p.Enqueue(proxies[0], c)
		p.Enqueue(proxies[0], d)
		p.MarkDone(proxies[0])
		p.Enqueue(proxies[0], a)
		_, req, _ := p.Dequeue()
		if !req.ConfigsUpdated.Equals(sets.New(key("a"), key("c"), key("d"))) {
			t.Fatalf("unexpected configs %v", req.ConfigsUpdated)
		}

		for name, req := range map[string]*model.PushRequest{"a": a, "b": b, "c": c, "d": d} {
			if !req.ConfigsUpdated.Equals(sets.New(key(name))) || req.Reason.Count() != 1 {
				t.Fatalf("shared request %v was modified: %v", name, req)
			}
		}
	})

	t.Run("concurrent merges with concurrent readers", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		const (
			conns     = 20
			producers = 4
			perProd   = 100
			total     = producers * perProd
		)
		cons := make([]*Connection, 0, conns)
		for i := range conns {
			con := newConnection("", nil)
			con.SetID(fmt.Sprintf("stress-%d", i))
			cons = append(cons, con)
		}
		// Share requests across connections, as StartPush does. Missing ConfigsUpdated also exercises
		// map allocation in CopyMergeInPlace.
		newReq := func(prod, i int) *model.PushRequest {
			id := fmt.Sprintf("%d-%d", prod, i)
			req := &model.PushRequest{
				AddressesUpdated: sets.New(id),
				WaypointsUpdated: sets.New(model.WaypointReference{Namespace: "ns", Hostname: id}),
				Reason:           model.NewReasonStats(model.EndpointUpdate),
			}
			if i%7 != 0 {
				req.ConfigsUpdated = sets.New(model.ConfigKey{Kind: kind.Endpoints, Name: id})
			}
			return req
		}
		reqs := make([][]*model.PushRequest, producers)
		for prod := range producers {
			for i := range perProd {
				reqs[prod] = append(reqs[prod], newReq(prod, i))
			}
		}

		var mu sync.Mutex
		received := map[*Connection]map[string]int{}
		reasons := map[*Connection]int{}
		var delivered sync.WaitGroup
		delivered.Add(conns * total)
		read := func(req *model.PushRequest) int {
			// Read shared maps through a shallow copy, as filterRelevantUpdates does.
			cp := *req
			n := cp.Reason.Count()
			for range cp.ConfigsUpdated {
				n++
			}
			for range cp.WaypointsUpdated {
				n++
			}
			return n
		}
		go func() {
			sem := make(chan struct{}, 8)
			for {
				sem <- struct{}{}
				con, req, shutdown := p.Dequeue()
				if shutdown {
					return
				}
				go func() {
					defer func() { <-sem }()
					read(req)
					mu.Lock()
					if received[con] == nil {
						received[con] = map[string]int{}
					}
					for addr := range req.AddressesUpdated {
						received[con][addr]++
					}
					reasons[con] += req.Reason.Count()
					mu.Unlock()
					for range req.AddressesUpdated {
						delivered.Done()
					}
					time.Sleep(time.Duration(len(req.AddressesUpdated)) * time.Microsecond)
					p.MarkDone(con)
				}()
			}
		}()

		var produced sync.WaitGroup
		for prod := range producers {
			produced.Add(1)
			go func() {
				defer produced.Done()
				for _, req := range reqs[prod] {
					for _, con := range cons {
						p.Enqueue(con, req)
					}
					// Other goroutines read shared requests concurrently, like other connections' pushes.
					read(req)
				}
			}()
		}
		produced.Wait()
		if !wgDoneOrTimeout(&delivered, 10*time.Second) {
			t.Fatal("timed out waiting for all updates to be delivered")
		}

		mu.Lock()
		defer mu.Unlock()
		for _, con := range cons {
			if len(received[con]) != total {
				t.Fatalf("%v: got %d distinct updates, want %d", con.ID(), len(received[con]), total)
			}
			for addr, n := range received[con] {
				if n != 1 {
					t.Fatalf("%v: update %v delivered %d times", con.ID(), addr, n)
				}
			}
			if reasons[con] != total {
				t.Fatalf("%v: got %d reasons, want %d", con.ID(), reasons[con], total)
			}
		}
		for prod := range producers {
			for i, req := range reqs[prod] {
				want := newReq(prod, i)
				if !req.ConfigsUpdated.Equals(want.ConfigsUpdated) || !req.AddressesUpdated.Equals(want.AddressesUpdated) ||
					!req.WaypointsUpdated.Equals(want.WaypointsUpdated) || req.Reason.Count() != 1 {
					t.Fatalf("shared request %d-%d was modified: %v", prod, i, req)
				}
			}
		}
	})

	t.Run("two removes, one should block one should return", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		wg := &sync.WaitGroup{}
		wg.Add(2)
		respChannel := make(chan *Connection, 2)
		go func() {
			respChannel <- getWithTimeout(p)
			wg.Done()
		}()
		time.Sleep(time.Millisecond * 50)
		p.Enqueue(proxies[0], &model.PushRequest{})
		go func() {
			respChannel <- getWithTimeout(p)
			wg.Done()
		}()

		wg.Wait()
		timeouts := 0
		close(respChannel)
		for resp := range respChannel {
			if resp == nil {
				timeouts++
			}
		}
		if timeouts != 1 {
			t.Fatalf("Expected 1 timeout, got %v", timeouts)
		}
	})

	t.Run("concurrent", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		key := func(p *Connection, eds string) string { return fmt.Sprintf("%s~%s", p.ID(), eds) }

		// We will trigger many pushes for eds services to each proxy. In the end we will expect
		// all of these to be dequeue, but order is not deterministic.
		expected := sets.String{}
		for eds := 0; eds < 100; eds++ {
			for _, pr := range proxies {
				expected.Insert(key(pr, fmt.Sprintf("%d", eds)))
			}
		}
		go func() {
			for eds := 0; eds < 100; eds++ {
				for _, pr := range proxies {
					p.Enqueue(pr, &model.PushRequest{
						ConfigsUpdated: sets.New(model.ConfigKey{
							Kind: kind.ServiceEntry,
							Name: fmt.Sprintf("%d", eds),
						}),
					})
				}
			}
		}()

		done := make(chan struct{})
		mu := sync.RWMutex{}
		go func() {
			for {
				con, info, shuttingdown := p.Dequeue()
				if shuttingdown {
					return
				}
				for eds := range model.ConfigNamesOfKind(info.ConfigsUpdated, kind.ServiceEntry) {
					mu.Lock()
					delete(expected, key(con, eds))
					mu.Unlock()
				}
				p.MarkDone(con)
				if len(expected) == 0 {
					done <- struct{}{}
				}
			}
		}()

		select {
		case <-done:
		case <-time.After(time.Second * 10):
			mu.RLock()
			defer mu.RUnlock()
			t.Fatalf("failed to get all updates, still pending: %v", len(expected))
		}
	})

	t.Run("concurrent with deterministic order", func(t *testing.T) {
		t.Parallel()
		p := NewPushQueue()
		defer p.ShutDown()

		con := newConnection("", nil)
		con.SetID("proxy-test")

		// We will trigger many pushes for eds services to the proxy. In the end we will expect
		// all of these to be dequeue, but order is deterministic.
		expected := make([]string, 100)
		for eds := 0; eds < 100; eds++ {
			expected[eds] = fmt.Sprintf("%d", eds)
		}
		go func() {
			// send to pushQueue
			for eds := 0; eds < 100; eds++ {
				p.Enqueue(con, &model.PushRequest{
					ConfigsUpdated: sets.New(model.ConfigKey{
						Kind: kind.Kind(eds),
						Name: fmt.Sprintf("%d", eds),
					}),
				})
			}
		}()

		processed := make([]string, 0, 100)
		done := make(chan struct{})
		pushChannel := make(chan *model.PushRequest)
		go func() {
			// dequeue pushQueue and send to pushChannel
			for {
				_, request, shuttingdown := p.Dequeue()
				if shuttingdown {
					close(pushChannel)
					return
				}
				pushChannel <- request
			}
		}()

		go func() {
			// recv from pushChannel and simulate push
			for {
				request := <-pushChannel
				if request == nil {
					return
				}
				updated := make([]string, 0, len(request.ConfigsUpdated))
				for configkey := range request.ConfigsUpdated {
					updated = append(updated, fmt.Sprintf("%d", configkey.Kind))
				}
				sort.Slice(updated, func(i, j int) bool {
					l, _ := strconv.Atoi(updated[i])
					r, _ := strconv.Atoi(updated[j])
					return l < r
				})
				processed = append(processed, updated...)
				if len(processed) == 100 {
					done <- struct{}{}
				}
				p.MarkDone(con)
			}
		}()

		select {
		case <-done:
		case <-time.After(time.Second * 10):
			t.Fatalf("failed to get all updates, still pending:  got %v", len(processed))
		}

		if !reflect.DeepEqual(expected, processed) {
			t.Fatalf("expected order %v, but got %v", expected, processed)
		}
	})
}

// BenchmarkPushQueueEndpointFanout fans shared endpoint updates out to 1,000 connections before draining
// a fresh queue, exercising CopyMerge and CopyMergeInPlace as requests accumulate.
func BenchmarkPushQueueEndpointFanout(b *testing.B) {
	const connections = 1000
	cons := make([]*Connection, 0, connections)
	for i := range connections {
		con := newConnection("", nil)
		con.SetID(fmt.Sprintf("proxy-%d", i))
		cons = append(cons, con)
	}
	cases := []struct {
		updates  int
		services int
	}{
		{updates: 1, services: 1},
		{updates: 2, services: 100},
		{updates: 1, services: 100},
		{updates: 10, services: 10},
		{updates: 100, services: 100},
		// Repeated updates to the same service.
		{updates: 100, services: 1},
	}
	for _, tt := range cases {
		reqs := make([]*model.PushRequest, 0, tt.updates)
		// Batch services when there are fewer requests than services; otherwise repeat service keys.
		servicesPerUpdate := max(1, (tt.services+tt.updates-1)/tt.updates)
		for i := range tt.updates {
			configs := sets.NewWithLength[model.ConfigKey](servicesPerUpdate)
			for j := range servicesPerUpdate {
				configs.Insert(model.ConfigKey{
					Kind:      kind.Endpoints,
					Name:      fmt.Sprintf("svc-%d", (i*servicesPerUpdate+j)%tt.services),
					Namespace: "ns",
				})
			}
			reqs = append(reqs, &model.PushRequest{
				ConfigsUpdated: configs,
				Reason:         model.NewReasonStats(model.EndpointUpdate),
			})
		}
		b.Run(fmt.Sprintf("updates=%d/services=%d", tt.updates, tt.services), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				p := NewPushQueue()
				for _, req := range reqs {
					for _, con := range cons {
						p.Enqueue(con, req)
					}
				}
				for range cons {
					con, req, _ := p.Dequeue()
					if len(req.ConfigsUpdated) != tt.services {
						b.Fatalf("expected %d merged updates, got %d", tt.services, len(req.ConfigsUpdated))
					}
					p.MarkDone(con)
				}
			}
		})
	}
}
