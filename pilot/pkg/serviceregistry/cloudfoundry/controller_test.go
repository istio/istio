// Copyright 2017 Istio Authors
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

package cloudfoundry_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"code.cloudfoundry.org/copilot/api"
	"github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry"
	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry/fakes"
)

func TestController_Caching(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	ticker := make(fakeTicker)
	client := &fakes.CopilotClient{}
	client.RoutesReturns(&api.RoutesResponse{
		Routes: []*api.RouteWithBackends{
			{
				Hostname: "process-guid-a.cfapps.internal",
				Backends: &api.BackendSet{
					Backends: []*api.Backend{
						{
							Address: "10.10.1.5",
							Port:    61005,
						},
						{
							Address: "10.0.40.2",
							Port:    61008,
						},
					},
				},
			},
			{
				Hostname: "process-guid-b.cfapps.internal",
				Backends: &api.BackendSet{
					Backends: []*api.Backend{
						{
							Address: "10.0.50.4",
							Port:    61009,
						},
						{
							Address: "10.0.60.2",
							Port:    61001,
						},
					},
				},
			},
		},
	}, nil)

	// initialize object under test
	controller := &cloudfoundry.Controller{
		Client: client,
		Ticker: ticker,
	}

	ih1, ih2 := new(fakeInstanceHandler), new(fakeInstanceHandler)
	sh1, sh2 := new(fakeServiceHandler), new(fakeServiceHandler)

	controller.AppendInstanceHandler(ih1.Do)
	controller.AppendInstanceHandler(ih2.Do)
	controller.AppendServiceHandler(sh1.Do)
	controller.AppendServiceHandler(sh2.Do)

	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(stop)

	// checking no handlers are called before the ticker fires
	g.Consistently(client.RoutesCallCount, "100ms").Should(gomega.Equal(0))
	g.Consistently(ih1.callCount, "100ms").Should(gomega.Equal(0))
	g.Consistently(ih2.callCount, "100ms").Should(gomega.Equal(0))
	g.Consistently(sh1.callCount, "100ms").Should(gomega.Equal(0))
	g.Consistently(sh2.callCount, "100ms").Should(gomega.Equal(0))

	// checking that all handlers are called after the first ticker fires
	ticker <- time.Time{}
	g.Eventually(client.RoutesCallCount).Should(gomega.Equal(1))
	g.Eventually(ih1.callCount).Should(gomega.Equal(1))
	g.Eventually(ih2.callCount).Should(gomega.Equal(1))
	g.Eventually(sh1.callCount).Should(gomega.Equal(1))
	g.Eventually(sh2.callCount).Should(gomega.Equal(1))

	// checking that no handlers are called if the cached data is still valid
	ticker <- time.Time{}
	g.Eventually(client.RoutesCallCount, "100ms").Should(gomega.Equal(2))
	g.Consistently(ih1.callCount, "100ms").Should(gomega.Equal(1))
	g.Consistently(ih2.callCount, "100ms").Should(gomega.Equal(1))
	g.Consistently(sh1.callCount, "100ms").Should(gomega.Equal(1))
	g.Consistently(sh2.callCount, "100ms").Should(gomega.Equal(1))

	// checking that all handlers are called again when the cache is invalidated
	client.RoutesReturns(&api.RoutesResponse{
		Routes: []*api.RouteWithBackends{
			{
				Hostname: "other-process-guid-a.cfapps.internal",
				Backends: &api.BackendSet{
					Backends: []*api.Backend{
						{
							Address: "10.10.2.6",
							Port:    61006,
						},
						{
							Address: "10.0.41.3",
							Port:    61009,
						},
					},
				},
			},
			{
				Hostname: "process-guid-b.cfapps.internal",
				Backends: &api.BackendSet{
					Backends: []*api.Backend{
						{
							Address: "10.0.50.4",
							Port:    61009,
						},
						{
							Address: "10.0.60.2",
							Port:    61001,
						},
					},
				},
			},
		},
	}, nil)
	ticker <- time.Time{}
	g.Eventually(client.RoutesCallCount).Should(gomega.Equal(3))
	g.Eventually(ih1.callCount).Should(gomega.Equal(2))
	g.Eventually(ih2.callCount).Should(gomega.Equal(2))
	g.Eventually(sh1.callCount).Should(gomega.Equal(2))
	g.Eventually(sh2.callCount).Should(gomega.Equal(2))
}

func TestController_ClientErrors(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	ticker := make(fakeTicker)
	client := &fakes.CopilotClient{}
	client.RoutesReturns(nil, errors.New("potato"))

	// initialize object under test
	controller := &cloudfoundry.Controller{
		Client: client,
		Ticker: ticker,
	}

	ih1 := new(fakeInstanceHandler)
	sh1 := new(fakeServiceHandler)

	controller.AppendInstanceHandler(ih1.Do)
	controller.AppendServiceHandler(sh1.Do)

	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(stop)

	ticker <- time.Time{}

	// it does not call the handlers
	g.Consistently(ih1.callCount, "100ms").Should(gomega.Equal(0))
	g.Consistently(sh1.callCount, "100ms").Should(gomega.Equal(0))
}

type fakeTicker chan time.Time

func (f fakeTicker) Chan() <-chan time.Time {
	return f
}

func (f fakeTicker) Tick() {
	f <- time.Now()
}

func (f fakeTicker) Stop() {}

type fakeHandler struct {
	mutex sync.Mutex
	calls int
}

func (h *fakeHandler) callCount() int {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return h.calls
}

func (h *fakeHandler) recordCall() {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.calls++
}

type fakeInstanceHandler struct {
	fakeHandler
}

func (h *fakeInstanceHandler) Do(*model.ServiceInstance, model.Event) {
	h.recordCall()
}

type fakeServiceHandler struct {
	fakeHandler
}

func (h *fakeServiceHandler) Do(*model.Service, model.Event) {
	h.recordCall()
}
