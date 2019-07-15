package v2

import (
	"fmt"
	"sync"
	"testing"
	"time"
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
		proxies = append(proxies, &XdsConnection{ConID: string(p)})
	}

	t.Run("simple add and remove", func(t *testing.T) {
		p := NewPushQueue()
		p.Enqueue(proxies[0], &PushInformation{})
		p.Enqueue(proxies[1], &PushInformation{})

		ExpectDequeue(t, p, proxies[0])
		ExpectDequeue(t, p, proxies[1])
	})

	t.Run("remove too many", func(t *testing.T) {
		p := NewPushQueue()
		p.Enqueue(proxies[0], &PushInformation{})

		ExpectDequeue(t, p, proxies[0])
		ExpectTimeout(t, p)
	})

	t.Run("add multiple times", func(t *testing.T) {
		p := NewPushQueue()
		p.Enqueue(proxies[0], &PushInformation{})
		p.Enqueue(proxies[1], &PushInformation{})
		p.Enqueue(proxies[0], &PushInformation{})

		ExpectDequeue(t, p, proxies[0])
		ExpectDequeue(t, p, proxies[1])
		ExpectTimeout(t, p)
	})

	t.Run("add and remove", func(t *testing.T) {
		p := NewPushQueue()
		p.Enqueue(proxies[0], &PushInformation{})
		ExpectDequeue(t, p, proxies[0])
		p.Enqueue(proxies[0], &PushInformation{})
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
		p.Enqueue(proxies[0], &PushInformation{})
		wg.Wait()
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
		p.Enqueue(proxies[0], &PushInformation{})
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

		go func() {
			for _, pr := range proxies {
				p.Enqueue(pr, &PushInformation{})
			}
		}()
		errs := make(chan error)
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			expect := 0
			for {
				got, _ := p.Dequeue()
				if got != proxies[expect] {
					errs <- fmt.Errorf("expected proxy %v got %v", proxies[expect], got)
				}
				expect++
				if expect == len(proxies) {
					wg.Done()
					break
				}
			}
		}()
		go func() {
			for _, pr := range proxies {
				p.Enqueue(pr, &PushInformation{})
			}
		}()
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
	})
}
