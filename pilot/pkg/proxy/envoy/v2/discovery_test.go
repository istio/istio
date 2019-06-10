package v2

import (
	"context"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/log"
)

func mockNeedsPush(node *model.Proxy) bool {
	return true
}

func TestSendPushes(t *testing.T) {
	stopCh := make(chan struct{})
	semaphore := make(chan struct{}, 2)
	queue := NewPushQueue()
	proxies := make([]*XdsConnection, 0, 100)
	for p := 0; p < 100; p++ {
		proxies = append(proxies, &XdsConnection{
			ConID:       "1",
			pushChannel: make(chan *XdsEvent),
			stream:      &fakeStream{},
		})
		proxy := proxies[p]
		go func() {
			for {
				p := <-proxy.pushChannel
				<-semaphore
				log.Errorf("howardjohn: %v", p)
			}
		}()
	}

	go doSendPushes(stopCh, semaphore, queue, mockNeedsPush)

	queue.Add(proxies[0], &PushInformation{})
	time.Sleep(time.Second * 1)

	queue.Add(proxies[0], &PushInformation{})
	time.Sleep(time.Second * 1)

	queue.Add(proxies[0], &PushInformation{})
	time.Sleep(time.Second * 1)
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
