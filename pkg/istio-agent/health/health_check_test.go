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

package health

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"istio.io/api/networking/v1alpha3"
)

func TestWorkloadHealthChecker_PerformApplicationHealthCheck(t *testing.T) {

	tcpHealthChecker := NewWorkloadHealthChecker(&v1alpha3.ReadinessProbe{
		InitialDelaySeconds: 0,
		TimeoutSeconds:      1,
		PeriodSeconds:       1,
		SuccessThreshold:    1,
		FailureThreshold:    1,
		HealthCheckMethod: &v1alpha3.ReadinessProbe_TcpSocket{
			TcpSocket: &v1alpha3.TCPHealthCheckConfig{
				Host: "localhost",
				Port: 5991,
			},
		},
	})

	tcpHealthChan := make(chan *ProbeEvent)
	quitChan := make(chan struct{})

	expectedTCPEvents := [6]*ProbeEvent{
		{Healthy: true},
		{Healthy: false},
		{Healthy: true},
		{Healthy: false},
		{Healthy: true},
		{Healthy: false}}
	tcpHealthStatuses := [6]bool{true, false, true, false, true, false}

	tcpFinishedEvents := sync.WaitGroup{}

	// wait for go-ahead for state change
	go func() {
		for i := 0; i < len(tcpHealthStatuses); i++ {
			if tcpHealthStatuses[i] {
				// open port until we get confirmation that
				srv, _ := net.Listen("tcp", "localhost:5991")
				// add to delta and wait for event received
				tcpFinishedEvents.Add(1)
				tcpFinishedEvents.Wait()
				srv.Close()
			} else {
				// dont listen or anything, just wait for event to finish to
				// start next iteration
				tcpFinishedEvents.Add(1)
				tcpFinishedEvents.Wait()
			}
		}
	}()

	go tcpHealthChecker.PerformApplicationHealthCheck(tcpHealthChan, quitChan)

	for c := 0; c < len(expectedTCPEvents); c++ {
		probeEvent := <-tcpHealthChan
		tcpFinishedEvents.Done()
		if probeEvent.Healthy != expectedTCPEvents[c].Healthy {
			t.Errorf("%s: got event healthy: %v at idx %v when expected healthy: %v", "tcp", probeEvent.Healthy, c, expectedTCPEvents[c].Healthy)
		}
	}
	quitChan <- struct{}{}

	httpPath := "/test/health/check"
	httpHealthChecker := NewWorkloadHealthChecker(&v1alpha3.ReadinessProbe{
		InitialDelaySeconds: 0,
		TimeoutSeconds:      1,
		PeriodSeconds:       1,
		SuccessThreshold:    1,
		FailureThreshold:    1,
		HealthCheckMethod: &v1alpha3.ReadinessProbe_HttpGet{
			HttpGet: &v1alpha3.HTTPHealthCheckConfig{
				Path:   httpPath,
				Port:   65112,
				Scheme: "http",
				Host:   "127.0.0.1",
			},
		},
	})
	httpHealthChan := make(chan *ProbeEvent)

	expectedHTTPEvents := [4]*ProbeEvent{
		{Healthy: true},
		{Healthy: false},
		{Healthy: true},
		{Healthy: false},
	}
	httpHealthStatuses := [4]bool{true, false, true, false}

	mockListener, err := net.Listen("tcp", "127.0.0.1:65112")
	if err != nil {
		t.Errorf("unable to start mock listener: %v", err)
	}
	httpServerEventCount := 0
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if httpHealthStatuses[httpServerEventCount] {
			writer.WriteHeader(200)
			writer.Write([]byte("lit"))
		} else {
			writer.WriteHeader(500)
		}
		httpServerEventCount++
	}))
	srv.Listener.Close()
	srv.Listener = mockListener
	srv.Start()
	defer srv.Close()

	go httpHealthChecker.PerformApplicationHealthCheck(httpHealthChan, quitChan)

	for c := 0; c < len(expectedHTTPEvents); c++ {
		probeEvent := <-httpHealthChan
		if probeEvent.Healthy != expectedHTTPEvents[c].Healthy {
			t.Errorf("expected event healthy: %v, got event healthy: %v at httpEvents idx %v", expectedHTTPEvents[c].Healthy, probeEvent.Healthy, c)
		}
	}
	quitChan <- struct{}{}
}
