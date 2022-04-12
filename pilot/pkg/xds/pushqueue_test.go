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
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/tests/util/leak"
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
		t.Fatalf("Expected timeout")
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
		t.Fatalf("Timed out")
	}
}

func TestProxyQueue(t *testing.T) {
	proxies := make([]*Connection, 0, 100)
	for p := 0; p < 100; p++ {
		proxies = append(proxies, &Connection{conID: fmt.Sprintf("proxy-%d", p)})
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
			Full: false,
			ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind: gvk.ServiceEntry,
				Name: "foo",
			}: {}},
			Start: firstTime,
		})

		p.Enqueue(proxies[0], &model.PushRequest{
			Full: false,
			ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind:      gvk.ServiceEntry,
				Name:      "bar",
				Namespace: "ns1",
			}: {}},
			Start: firstTime.Add(time.Second),
		})
		_, info, _ := p.Dequeue()

		if info.Start != firstTime {
			t.Errorf("Expected start time to be %v, got %v", firstTime, info.Start)
		}
		expectedEds := map[model.ConfigKey]struct{}{{
			Kind:      gvk.ServiceEntry,
			Name:      "foo",
			Namespace: "",
		}: {}, {
			Kind:      gvk.ServiceEntry,
			Name:      "bar",
			Namespace: "ns1",
		}: {}}
		if !reflect.DeepEqual(model.ConfigsOfKind(info.ConfigsUpdated, gvk.ServiceEntry), expectedEds) {
			t.Errorf("Expected EdsUpdates to be %v, got %v", expectedEds, model.ConfigsOfKind(info.ConfigsUpdated, gvk.ServiceEntry))
		}
		if info.Full {
			t.Errorf("Expected full to be false, got true")
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

		key := func(p *Connection, eds string) string { return fmt.Sprintf("%s~%s", p.conID, eds) }

		// We will trigger many pushes for eds services to each proxy. In the end we will expect
		// all of these to be dequeue, but order is not deterministic.
		expected := map[string]struct{}{}
		for eds := 0; eds < 100; eds++ {
			for _, pr := range proxies {
				expected[key(pr, fmt.Sprintf("%d", eds))] = struct{}{}
			}
		}
		go func() {
			for eds := 0; eds < 100; eds++ {
				for _, pr := range proxies {
					p.Enqueue(pr, &model.PushRequest{
						ConfigsUpdated: map[model.ConfigKey]struct{}{{
							Kind: gvk.ServiceEntry,
							Name: fmt.Sprintf("%d", eds),
						}: {}},
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
				for eds := range model.ConfigNamesOfKind(info.ConfigsUpdated, gvk.ServiceEntry) {
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
		con := &Connection{conID: "proxy-test"}

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
					ConfigsUpdated: map[model.ConfigKey]struct{}{{
						Kind: config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: fmt.Sprintf("%d", eds)},
						Name: fmt.Sprintf("%d", eds),
					}: {}},
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
					updated = append(updated, configkey.Kind.Kind)
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

// TestPushQueueLeak is a regression test for https://github.com/grpc/grpc-go/issues/4758
func TestPushQueueLeak(t *testing.T) {
	ds := NewFakeDiscoveryServer(t, FakeOptions{})
	p := ds.ConnectADS()
	p.RequestResponseAck(t, nil)
	for _, c := range ds.Discovery.AllClients() {
		leak.MustGarbageCollect(t, c)
	}
	ds.Discovery.startPush(&model.PushRequest{})
	p.Cleanup()
}
