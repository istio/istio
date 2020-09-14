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
		HealthCheckMethod:   &v1alpha3.ReadinessProbe_TcpSocket{
			TcpSocket: &v1alpha3.TCPHealthCheckConfig{
				Host: "localhost",
				Port: 5991,
			},
		},
	})

	tcpHealthChan := make(chan *ProbeEvent)
	quitChan := make(chan struct{})

	expectedEvents := [6]*ProbeEvent{
		{Healthy: true},
		{Healthy: false},
		{Healthy: true},
		{Healthy: false},
		{Healthy: true},
		{Healthy: false}}
	healthStatuses := [6]bool{true, false, true, false, true, false}

	finishedEventRequest := sync.WaitGroup{}

	// wait for go-ahead for state change
	go func() {
		for i := 0; i < len(healthStatuses); i++ {
			if healthStatuses[i] {
				// open port until we get confirmation that
				srv, _ := net.Listen("tcp", "localhost:5991")
				// add to delta and wait for event received
				finishedEventRequest.Add(1)
				finishedEventRequest.Wait()
				srv.Close()
			} else {
				// dont listen or anything, just wait for event to finish to
				// start next iteration
				finishedEventRequest.Add(1)
				finishedEventRequest.Wait()
			}
		}
	}()

	go tcpHealthChecker.PerformApplicationHealthCheck(tcpHealthChan, quitChan)

	for c := 0; c < len(expectedEvents); c++ {
		probeEvent := <-tcpHealthChan
		finishedEventRequest.Done()
		if probeEvent.Healthy != expectedEvents[c].Healthy {
			t.Errorf("%s: got event healthy: %v at idx %v when expected healthy: %v", "tcp", probeEvent.Healthy, c, expectedEvents[c].Healthy)
		}
	}
	quitChan <- struct{}{}
}
