// Copyright 2018 Istio Authors
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

package test

/*
Package test implements the boiler plate for testing model.Controller implementations.

Typical usage:

    import "istio.io/istio/pilot/platform/test"

    func TestController(t *testing.T) {
    	mockHandler := test.NewMockControllerViewHandler()
    	controller, err := NewYourFavoriteController(...., *mockHandler.GetTicker())
    	if err != nil {
    		t.Fatalf("could not create My favourite Controller: %v", err)
    	}
        mockHandler.AssertControllerOK(t, controller,
            test.BuildExpectedControllerView(yourExpectedServices, yourExpectedInstances))
    }
*/

import (
	"encoding/hex"
	"net"
	"testing"
	"time"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pkg/log"
)

const (
	MockControllerPath        = "/somepath/"
	maxPendingReconciledViews = 1000
	maxWaitForReconciledView  = 60 * time.Second
)

// MockControllerViewHandler is a mock of model.ControllerViewHandler and is intended
// for testing public interfaces of controllers under istio.io/istio/pilot/platform/*
type MockControllerViewHandler struct {
	runTicker       time.Ticker
	stop            chan struct{}
	reconciledViews chan *model.ControllerView
}

// Creates a MockControllerViewHandler for testing public interfaces of controllers
// under istio.io/istio/pilot/platform/*
func NewMockControllerViewHandler() *MockControllerViewHandler {
	return NewMockControllerViewHandlerWithTicker(time.Nanosecond)
}

// Creates a MockControllerViewHandler for testing public interfaces of controllers
// under istio.io/istio/pilot/platform/*
func NewMockControllerViewHandlerWithTicker(tickerDuration time.Duration) *MockControllerViewHandler {
	return &MockControllerViewHandler{
		runTicker:       *time.NewTicker(tickerDuration),
		stop:            make(chan struct{}, 1),
		reconciledViews: make(chan *model.ControllerView, maxPendingReconciledViews),
	}
}

// Retrieves the ticker used for creating controllers under istio.io/istio/pilot/platform/*
// The returned ticker ticks every nanosecond to allow model.Controller.Run() to execute
// without holding up tests.
func (h *MockControllerViewHandler) GetTicker() *time.Ticker {
	return &h.runTicker
}

// Retrieves the stop channel used for passing to model.Controller.Run() methods of
// controllers under istio.io/istio/pilot/platform/*. Callers of this method ought
// to call defer h.StopController() to cause controllers to stop running.
func (h *MockControllerViewHandler) getStopChannel() chan struct{} {
	return h.stop
}

// Issues a signal to the stop channel for stopping test controllers under
// istio.io/istio/pilot/platform/*. Callers of this method ought
// to have passed the contents of GetStopChannel() to the controllers to begin with.
func (h *MockControllerViewHandler) stopController() {
	h.runTicker.Stop()
	close(h.stop)
	close(h.reconciledViews)
}

// Implements model.ControllerViewHandler. Not intended for direct use in test code
func (h *MockControllerViewHandler) Reconcile(cv *model.ControllerView) {
	defer func() {
		// Ignore errors on channel close
		recover()
	}()
	select {
	case h.reconciledViews <- cv:
		break
	default:
		log.Errorf("Possible test setup issue: Too many views '%d' reconciled but not retreived by the test",
			maxPendingReconciledViews)
		panic("Test setup issue. Too many views sent to Reconcile mesh view")
	}
}

// Gets the most recently supplied controller view to Reconcile()
func (h *MockControllerViewHandler) getReconciledView() *model.ControllerView {
	timeout := make(chan bool, 1)
	go func() {
		time.Sleep(maxWaitForReconciledView)
		timeout <- true
	}()
	for {
		select {
		case cv, ok := <-h.reconciledViews:
			if !ok {
				return nil
			}
			return cv
		case _, ok := <-timeout:
			if !ok {
				return nil
			}
			log.Errorf("Unable to get reconciled view in '%v'.",
				maxWaitForReconciledView)
			panic("Timedout waiting for mesh view to reconcile")
		default:
			time.Sleep(time.Millisecond * 5)
		}
	}
	// Should never happen
	return nil
}

// Used to pass to mock.Controller.Handle()
func (h *MockControllerViewHandler) toModelControllerViewHandler() *model.ControllerViewHandler {
	var handler model.ControllerViewHandler
	handler = h
	return &handler
}

// Verifies that the supplied controller is able to provide an actual controller view that matches
// up with the expected controller view ev.
func (h *MockControllerViewHandler) AssertControllerOK(t *testing.T, c model.Controller, ev *model.ControllerView) {
	c.Handle(MockControllerPath, h.toModelControllerViewHandler())
	go c.Run(h.getStopChannel())
	defer h.stopController()
	// Until we receive non-empty service list or there's a timeout
	for {
		actualView := h.getReconciledView()
		if actualView == nil {
			// Channel was closed
			t.Fatal("Unexpected nil reconciled view. Channels used by handler may have been closed prematurely.")

		}
		if len(actualView.Services) > 0 {
			assertControllerViewEquals(t, ev, actualView)
			break
		}
	}
}

// Builds the expected model.ControllerView given the list of services and instances provided by the platform
func BuildExpectedControllerView(modelSvcs []*model.Service, modelInsts []*model.ServiceInstance) *model.ControllerView {
	return &model.ControllerView{
		Path:             MockControllerPath,
		Services:         modelSvcs,
		ServiceInstances: modelInsts,
	}
}

// buildServiceInstanceKey builds a key to a service instance for a specific registry
// Format for service instance: [service name][hex value of IP address][hex value of port number]
// Within the mesh there can be exactly one endpoint for a service with the combination of
// IP address and port.
func buildServiceInstanceKey(i *model.ServiceInstance) string {
	return "[" + i.Service.Hostname + "][" + getIPHex(i.Endpoint.Address) + "][" + getPortHex(i.Endpoint.Port) + "]"
}

func getIPHex(address string) string {
	ip := net.ParseIP(address)
	return hex.EncodeToString(ip)
}

func getPortHex(port int) string {
	pb := []byte{byte((port >> 8) & 0xFF), byte(port & 0xFF)}
	return hex.EncodeToString(pb)
}

func printService(svc *model.Service) string {
	out := svc.Hostname
	if svc.Address != "" {
		out = out + "@" + svc.Address
	}
	return out
}

func printServiceList(l []*model.Service) string {
	out := "["
	for _, svc := range l {
		out = out + printService(svc) + ","
	}
	out = out + "]"
	return out
}

func assertControllerViewEquals(t *testing.T, expected, actual *model.ControllerView) {
	t.Run("CheckServices", func(t *testing.T) {
		if len(expected.Services) != len(actual.Services) {
			t.Errorf("Expecting services with %s. Actual services %s",
				printServiceList(expected.Services), printServiceList(actual.Services))
		}
		expSvcsMap := map[string]*model.Service{}
		for _, svc := range expected.Services {
			expSvcsMap[svc.Hostname] = svc
		}
		actSvcsMap := map[string]*model.Service{}
		for _, svc := range actual.Services {
			actSvcsMap[svc.Hostname] = svc
		}
		if len(expSvcsMap) != len(actSvcsMap) {
			t.Errorf("Expecting services with %s. Actual services %s",
				printServiceList(expected.Services), printServiceList(actual.Services))
		}
		for hostname, svc := range expSvcsMap {
			act, found := actSvcsMap[hostname]
			if !found {
				t.Errorf("Expected service with hostname '%s', found none. Expected services with %s. Actual services %s",
					hostname, printServiceList(expected.Services), printServiceList(actual.Services))

				continue
			}
			if !svc.Equals(act) {
				t.Errorf("Service with hostname '%s' do not match. Expected service %s. Actual service %s",
					hostname, printService(svc), printService(act))
			}
		}
	})
	t.Run("CheckServiceInstances", func(t *testing.T) {
		if len(expected.ServiceInstances) != len(actual.ServiceInstances) {
			t.Errorf("Expecting service instances with %v. Actual service instances %v",
				expected.ServiceInstances, actual.ServiceInstances)
		}
		expInstMap := map[string]*model.ServiceInstance{}
		for _, inst := range expected.ServiceInstances {
			expInstMap[buildServiceInstanceKey(inst)] = inst
		}
		actInstMap := map[string]*model.ServiceInstance{}
		for _, inst := range expected.ServiceInstances {
			actInstMap[buildServiceInstanceKey(inst)] = inst
		}
		if len(expInstMap) != len(actInstMap) {
			t.Errorf("Expecting service instances with %v. Actual service instances %v",
				expected.ServiceInstances, actual.ServiceInstances)
		}
		for instKey, inst := range expInstMap {
			act, found := actInstMap[instKey]
			if !found {
				t.Errorf("Expected service instance identified by '%s', found none. Expected service instances with %v. Actual service instances %v",
					instKey, expected.ServiceInstances, actual.ServiceInstances)
				continue
			}
			if !inst.Equals(act) {
				t.Errorf("Service isntance identified by '%s' do not match. Expected service instance %v. Actual service instance %v",
					instKey, *inst, *act)
			}
		}
	})
}
