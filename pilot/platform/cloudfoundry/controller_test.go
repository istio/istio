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
	"sync"
	"time"

	"code.cloudfoundry.org/copilot/api"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform/cloudfoundry"
)

var _ = Describe("Controller", func() {
	var (
		ticker     fakeTicker
		client     *mockCopilotClient
		controller *cloudfoundry.Controller
	)

	BeforeEach(func() {
		ticker = make(fakeTicker)
		client = newMockCopilotClient()
		client.RoutesOutput.Ret0 <- &api.RoutesResponse{
			Backends: map[string]*api.BackendSet{
				"process-guid-a.cfapps.internal": {
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
				"process-guid-b.cfapps.internal": {
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
		}
		client.RoutesOutput.Ret1 <- nil
		controller = &cloudfoundry.Controller{
			Client: client,
			Ticker: ticker,
		}
	})

	It("calls all the instance handlers when the cache is invalidated", func() {
		ih1, ih2 := new(fakeInstanceHandler), new(fakeInstanceHandler)
		sh1, sh2 := new(fakeServiceHandler), new(fakeServiceHandler)

		controller.AppendInstanceHandler(ih1.Do)
		controller.AppendInstanceHandler(ih2.Do)
		controller.AppendServiceHandler(sh1.Do)
		controller.AppendServiceHandler(sh2.Do)

		stop := make(chan struct{})
		defer close(stop)
		go controller.Run(stop)

		By("checking no handlers are called before the ticker fires")
		Consistently(client.RoutesCalled, "100ms").ShouldNot(Receive())
		Consistently(ih1.callCount, "100ms").Should(Equal(0))
		Consistently(ih2.callCount, "100ms").Should(Equal(0))
		Consistently(sh1.callCount, "100ms").Should(Equal(0))
		Consistently(sh2.callCount, "100ms").Should(Equal(0))

		By("checking that all handlers are called after the first ticker fires")
		ticker <- time.Time{}
		Eventually(client.RoutesCalled).Should(Receive())
		Eventually(ih1.callCount).Should(Equal(1))
		Eventually(ih2.callCount).Should(Equal(1))
		Eventually(sh1.callCount).Should(Equal(1))
		Eventually(sh2.callCount).Should(Equal(1))

		By("checking that no handlers are called if the cached data is still valid")
		ticker <- time.Time{}
		Eventually(client.RoutesCalled).Should(Receive())
		Consistently(ih1.callCount, "100ms").Should(Equal(1))
		Consistently(ih2.callCount, "100ms").Should(Equal(1))
		Consistently(sh1.callCount, "100ms").Should(Equal(1))
		Consistently(sh2.callCount, "100ms").Should(Equal(1))

		By("checking that all handlers are called again when the cache is invalidated")
		client.RoutesOutput.Ret0 <- &api.RoutesResponse{
			Backends: map[string]*api.BackendSet{
				"other-process-guid-a.cfapps.internal": {
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
				"process-guid-b.cfapps.internal": {
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
		}
		client.RoutesOutput.Ret1 <- nil
		ticker <- time.Time{}
		Eventually(client.RoutesCalled).Should(Receive())
		Eventually(ih1.callCount).Should(Equal(2))
		Eventually(ih2.callCount).Should(Equal(2))
		Eventually(sh1.callCount).Should(Equal(2))
		Eventually(sh2.callCount).Should(Equal(2))
	})
})

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
