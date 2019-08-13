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
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
)

// Helper function to remove an item or timeout and return nil if there are no pending pushes
func getWithTimeout(p *PushQueue) *XdsConnection {
	done := make(chan *XdsConnection)
	go func() {
		con, _ := p.Dequeue()
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
	done := make(chan struct{})
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

func ExpectDequeue(t *testing.T, p *PushQueue, expected *XdsConnection) {
	t.Helper()
	result := make(chan *XdsConnection)
	go func() {
		con, _ := p.Dequeue()
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
	proxies := make([]*XdsConnection, 0, 100)
	for p := 0; p < 100; p++ {
		proxies = append(proxies, &XdsConnection{ConID: fmt.Sprintf("proxy-%d", p)})
	}

	t.Run("simple add and remove", func(t *testing.T) {
		p := NewPushQueue()
		p.Enqueue(proxies[0], &model.PushRequest{})
		p.Enqueue(proxies[1], &model.PushRequest{})

		ExpectDequeue(t, p, proxies[0])
		ExpectDequeue(t, p, proxies[1])
	})

	t.Run("remove too many", func(t *testing.T) {
		p := NewPushQueue()
		p.Enqueue(proxies[0], &model.PushRequest{})

		ExpectDequeue(t, p, proxies[0])
		ExpectTimeout(t, p)
	})

	t.Run("add multiple times", func(t *testing.T) {
		p := NewPushQueue()
		p.Enqueue(proxies[0], &model.PushRequest{})
		p.Enqueue(proxies[1], &model.PushRequest{})
		p.Enqueue(proxies[0], &model.PushRequest{})

		ExpectDequeue(t, p, proxies[0])
		ExpectDequeue(t, p, proxies[1])
		ExpectTimeout(t, p)
	})

	t.Run("add and remove and markdone", func(t *testing.T) {
		p := NewPushQueue()
		p.Enqueue(proxies[0], &model.PushRequest{})
		ExpectDequeue(t, p, proxies[0])
		p.MarkDone(proxies[0])
		p.Enqueue(proxies[0], &model.PushRequest{})
		ExpectDequeue(t, p, proxies[0])
		ExpectTimeout(t, p)
	})

	t.Run("add and remove and add and markdone", func(t *testing.T) {
		p := NewPushQueue()
		p.Enqueue(proxies[0], &model.PushRequest{})
		ExpectDequeue(t, p, proxies[0])
		p.Enqueue(proxies[0], &model.PushRequest{})
		p.Enqueue(proxies[0], &model.PushRequest{})
		p.MarkDone(proxies[0])
		ExpectDequeue(t, p, proxies[0])
		ExpectTimeout(t, p)
	})

	t.Run("remove should block", func(t *testing.T) {
		p := NewPushQueue()
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
		p := NewPushQueue()
		firstTime := time.Now()
		p.Enqueue(proxies[0], &model.PushRequest{
			Full:       false,
			EdsUpdates: map[string]struct{}{"foo": {}},
			Start:      firstTime,
		})

		p.Enqueue(proxies[0], &model.PushRequest{
			Full:       false,
			EdsUpdates: map[string]struct{}{"bar": {}},
			Start:      firstTime.Add(time.Second),
		})
		_, info := p.Dequeue()

		if info.Start != firstTime {
			t.Errorf("Expected start time to be %v, got %v", firstTime, info.Start)
		}
		expectedEds := map[string]struct{}{"foo": {}, "bar": {}}
		if !reflect.DeepEqual(info.EdsUpdates, expectedEds) {
			t.Errorf("Expected EdsUpdates to be %v, got %v", expectedEds, info.EdsUpdates)
		}
		if info.Full != false {
			t.Errorf("Expected full to be false, got true")
		}
	})

	t.Run("two removes, one should block one should return", func(t *testing.T) {
		p := NewPushQueue()
		wg := &sync.WaitGroup{}
		wg.Add(2)
		respChannel := make(chan *XdsConnection, 2)
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
		p := NewPushQueue()
		key := func(p *XdsConnection, eds string) string { return fmt.Sprintf("%s~%s", p.ConID, eds) }

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
					p.Enqueue(pr, &model.PushRequest{EdsUpdates: map[string]struct{}{
						fmt.Sprintf("%d", eds): {},
					}})
				}
			}
		}()

		done := make(chan struct{})
		mu := sync.RWMutex{}
		go func() {
			for {
				con, info := p.Dequeue()
				for eds := range info.EdsUpdates {
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
}
