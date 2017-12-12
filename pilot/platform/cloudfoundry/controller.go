package cloudfoundry

import (
	"reflect"
	"time"

	"golang.org/x/net/context"

	copilotapi "code.cloudfoundry.org/copilot/api"
	"github.com/golang/glog"

	"istio.io/istio/pilot/model"
)

type serviceHandler func(*model.Service, model.Event)
type instanceHandler func(*model.ServiceInstance, model.Event)

type Ticker interface {
	Chan() <-chan time.Time
	Stop()
}

type realTicker struct {
	*time.Ticker
}

func (r realTicker) Chan() <-chan time.Time {
	return r.C
}

func NewTicker(d time.Duration) Ticker {
	return realTicker{time.NewTicker(d)}
}

type Controller struct {
	Client           copilotapi.IstioCopilotClient
	Ticker           Ticker
	serviceHandlers  []serviceHandler
	instanceHandlers []instanceHandler
}

func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.serviceHandlers = append(c.serviceHandlers, f)
	return nil
}

func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instanceHandlers = append(c.instanceHandlers, f)
	return nil
}

func (c *Controller) Run(stop <-chan struct{}) {
	cache := &copilotapi.RoutesResponse{}
	for {
		select {
		case <-c.Ticker.Chan():
			backendSets, err := c.Client.Routes(context.Background(), &copilotapi.RoutesRequest{})
			if err != nil {
				glog.Warningf("periodic copilot routes poll failed: %s", err)
				continue
			}

			if !reflect.DeepEqual(backendSets, cache) {
				cache = backendSets
				// Clear service discovery cache
				for _, h := range c.serviceHandlers {
					go h(&model.Service{}, model.EventAdd)
				}
				for _, h := range c.instanceHandlers {
					go h(&model.ServiceInstance{}, model.EventAdd)
				}
			}
		case <-stop:
			c.Ticker.Stop()
			return
		}
	}
}
