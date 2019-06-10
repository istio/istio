package v2

import (
	"sync"
	"testing"
	"time"
)

// Helper function to remove an item or timeout and return nil if there are no pending pushes
func getWithTimeout(p *PushQueue) *XdsConnection {
	done := make(chan *XdsConnection)
	go func() {
		con, _ := p.Remove()
		done <- con
	}()
	select {
	case ret := <-done:
		return ret
	case <-time.After(time.Millisecond * 500):
		return nil
	}
}

func TestProxyQueue(t *testing.T) {
	proxies := make([]*XdsConnection, 0, 100)
	for p := 0; p < 100; p++ {
		proxies = append(proxies, &XdsConnection{ConID: string(p)})
	}

	ExpectTimeout := func(p *PushQueue) {
		done := make(chan struct{})
		go func() {
			p.Remove()
			done <- struct{}{}
		}()
		select {
		case <-done:
			t.Fatalf("Expected timeout")
		case <-time.After(time.Millisecond * 500):
		}
	}

	ExpectPop := func(p *PushQueue, expected *XdsConnection) {
		if got, _ := p.Remove(); got != expected {
			t.Fatalf("Expected proxy %v, got %v", expected, got)
		}
	}

	t.Run("simple add and remove", func(t *testing.T) {
		p := NewPushQueue()
		p.Add(proxies[0], &PushInformation{})
		p.Add(proxies[1], &PushInformation{})

		ExpectPop(p, proxies[0])
		ExpectPop(p, proxies[1])
	})

	t.Run("remove too many", func(t *testing.T) {
		p := NewPushQueue()
		p.Add(proxies[0], &PushInformation{})

		ExpectPop(p, proxies[0])
		ExpectTimeout(p)
	})

	t.Run("add multiple times", func(t *testing.T) {
		p := NewPushQueue()
		p.Add(proxies[0], &PushInformation{})
		p.Add(proxies[1], &PushInformation{})
		p.Add(proxies[0], &PushInformation{})

		ExpectPop(p, proxies[0])
		ExpectPop(p, proxies[1])
		ExpectTimeout(p)
	})

	t.Run("add and remove", func(t *testing.T) {
		p := NewPushQueue()
		p.Add(proxies[0], &PushInformation{})
		ExpectPop(p, proxies[0])
		p.Add(proxies[0], &PushInformation{})
		ExpectPop(p, proxies[0])
		ExpectTimeout(p)
	})

	t.Run("remove should block", func(t *testing.T) {
		p := NewPushQueue()
		wg := &sync.WaitGroup{}
		wg.Add(1)
		go func() {
			ExpectPop(p, proxies[0])
			wg.Done()
		}()
		time.Sleep(time.Millisecond * 50)
		p.Add(proxies[0], &PushInformation{})
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
		go func() {
			respChannel <- getWithTimeout(p)
			wg.Done()
		}()
		time.Sleep(time.Millisecond * 50)
		p.Add(proxies[0], &PushInformation{})
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
				p.Add(pr, &PushInformation{})
			}
		}()
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			expect := 0
			for {
				got, _ := p.Remove()
				if got != proxies[expect] {
					t.Fatalf("Expected proxy %v got %v", proxies[expect], got)
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
				p.Add(pr, &PushInformation{})
			}
		}()
		wg.Wait()
	})
}
