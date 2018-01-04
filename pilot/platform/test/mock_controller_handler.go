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
    
	"github.com/golang/glog"
	"istio.io/istio/pilot/model"
)

const (
    MockControllerPath = "/somepath/"
    maxPendingReconciledViews = 1000
    maxWaitForReconciledView  = 60 * time.Second
)


// MockControllerViewHandler is a mock of model.ControllerViewHandler and is intended 
// for testing public interfaces of controllers under istio.io/istio/pilot/platform/*
type MockControllerViewHandler struct {
    runTicker 		time.Ticker
    stop			chan struct{}
    reconciledViews chan *model.ControllerView
}

// Creates a MockControllerViewHandler for testing public interfaces of controllers
// under istio.io/istio/pilot/platform/*
func NewMockControllerViewHandler() *MockControllerViewHandler {
    return &MockControllerViewHandler{
        runTicker: 			*time.NewTicker(time.Nanosecond),
        stop:				make(chan struct{}, 1),
        reconciledViews:	make(chan *model.ControllerView, maxPendingReconciledViews),
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
    close(h.reconciledViews)
    close(h.stop)
}

// Implements model.ControllerViewHandler. Not intended for direct use in test code
func (h *MockControllerViewHandler) Reconcile(cv *model.ControllerView) {
    select {
    case h.reconciledViews <- cv:
        break
    default:
        glog.Fatalf("Possible test setup issue: Too many views '%d' reconciled but not retreived by the test", 
            maxPendingReconciledViews)
    } 
}

// Gets the most recently supplied controller view to Reconcile()
func (h *MockControllerViewHandler) getReconciledView() *model.ControllerView {
    timeout := make(chan bool, 1)
    go func() {
        time.Sleep(maxWaitForReconciledView)
        timeout <- true
    }()
    select {
    case cv := <-h.reconciledViews:
        return cv
    case <-timeout:
        glog.Fatalf("Unable to get reconciled view in '%v'.", 
            maxWaitForReconciledView)
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
	actualView := h.getReconciledView()
	assertControllerViewEquals(t, ev, actualView)    
}

// Builds the expected model.ControllerView given the list of services and instances provided by the platform
func BuildExpectedControllerView(modelSvcs []*model.Service, modelInsts []*model.ServiceInstance) *model.ControllerView { 
    return &model.ControllerView {
        Path: 				MockControllerPath,
        Services:			modelSvcs,
        ServiceInstances: 	modelInsts,
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

func assertControllerViewEquals(t *testing.T, expected, actual *model.ControllerView) {
    t.Run("CheckServices", func(t *testing.T) {
        if len(expected.Services) != len(actual.Services) {
            t.Errorf("Expecting services with %v. Actual services %v", 
                expected.Services, actual.Services)
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
            t.Errorf("Expecting services with %v. Actual services %v", 
                expected.Services, actual.Services)
        }
        for hostname, svc := range expSvcsMap {
            act, found := actSvcsMap[hostname]
            if !found {
                t.Errorf("Expected service with hostname '%s', found none. Expected services with %v. Actual services %v", 
                    hostname, expected, actual)
            }
            if !svc.Equals(act) {
                t.Errorf("Service with hostname '%s' do not match. Expected service %v. Actual service %v",
                    hostname, *svc, *act)
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
            }
            if !inst.Equals(act) {
                t.Errorf("Service isntance identified by '%s' do not match. Expected service instance %v. Actual service instance %v",
                    instKey, *inst, *act)
            }
        }
    })
}
