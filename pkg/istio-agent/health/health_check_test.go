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
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.uber.org/atomic"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

// DelayedListener is like net.Listener but delays the Listen() syscall.
// This allows reserving a port without listening.
type DelayedListener struct {
	fd   int
	port int
}

func NewDelayedListener() (*DelayedListener, error) {
	d := &DelayedListener{}
	addr := &syscall.SockaddrInet4{
		Port: 0,
		Addr: [4]byte{0, 0, 0, 0},
	}
	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_IP)
	if err != nil {
		return nil, err
	}
	d.fd = s
	if err := syscall.Bind(s, addr); err != nil {
		return nil, err
	}
	sa, err := syscall.Getsockname(s)
	if err != nil {
		return nil, err
	}
	d.port = sa.(*syscall.SockaddrInet4).Port
	return d, nil
}

func (d *DelayedListener) Address() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(d.port))
}

func (d *DelayedListener) Listen() error {
	if err := syscall.Listen(d.fd, 128); err != nil {
		return nil
	}
	return nil
}

func (d *DelayedListener) Close() error {
	if err := syscall.Close(d.fd); err != nil {
		return nil
	}
	return nil
}

func TestDelayedListener(t *testing.T) {
	d, err := NewDelayedListener()
	assert.NoError(t, err)

	// Should have connection refused before we listen
	_, err = net.Dial("tcp", d.Address())
	assert.Error(t, err)

	assert.NoError(t, d.Listen())

	// Now we should see success
	_, err = net.Dial("tcp", d.Address())
	assert.NoError(t, err)

	assert.NoError(t, d.Close())
	// Now we should failure again success
	_, err = net.Dial("tcp", d.Address())
	assert.Error(t, err)
}

func TestWorkloadHealthChecker_PerformApplicationHealthCheck(t *testing.T) {
	t.Run("tcp", func(t *testing.T) {
		listener, err := NewDelayedListener()
		assert.NoError(t, err)
		tcpHealthChecker := NewWorkloadHealthChecker(&v1alpha3.ReadinessProbe{
			InitialDelaySeconds: 0,
			TimeoutSeconds:      1,
			PeriodSeconds:       1,
			SuccessThreshold:    1,
			FailureThreshold:    1,
			HealthCheckMethod: &v1alpha3.ReadinessProbe_TcpSocket{
				TcpSocket: &v1alpha3.TCPHealthCheckConfig{
					Host: "localhost",
					Port: uint32(listener.port),
				},
			},
		}, nil, []string{"127.0.0.1"}, false)
		// Speed up tests
		tcpHealthChecker.config.CheckFrequency = time.Millisecond

		quitChan := make(chan struct{})

		expectedTCPEvents := [3]*ProbeEvent{
			{Healthy: false},
			{Healthy: true},
			{Healthy: false},
		}
		tcpHealthStatuses := [3]bool{false, true, false}

		cont := make(chan struct{}, len(expectedTCPEvents))
		// wait for go-ahead for state change
		go func() {
			prev := false
			for _, want := range tcpHealthStatuses {
				if !prev && want {
					assert.NoError(t, listener.Listen())
				}
				if prev && !want {
					assert.NoError(t, listener.Close())
				}
				<-cont
				prev = want
			}
		}()

		eventNum := atomic.NewInt32(0)
		go tcpHealthChecker.PerformApplicationHealthCheck(func(event *ProbeEvent) {
			if int(eventNum.Load()) >= len(tcpHealthStatuses) {
				return
			}
			if event.Healthy != expectedTCPEvents[eventNum.Load()].Healthy {
				t.Errorf("%s: got event healthy: %v at idx %v when expected healthy: %v", "tcp", event.Healthy, eventNum.Load(), expectedTCPEvents[eventNum.Load()].Healthy)
			}
			cont <- struct{}{}
			eventNum.Inc()
		}, quitChan)
		retry.UntilSuccessOrFail(t, func() error {
			if int(eventNum.Load()) != len(expectedTCPEvents) {
				return fmt.Errorf("waiting for %v events", len(expectedTCPEvents)-int(eventNum.Load()))
			}
			return nil
		}, retry.Delay(time.Millisecond*10), retry.Timeout(time.Second))
		close(quitChan)
	})
	t.Run("http", func(t *testing.T) {
		httpPath := "/test/health/check"
		httpHealthStatuses := [4]bool{true, false, false, true}
		httpServerEventCount := 0
		sss := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if httpServerEventCount < len(httpHealthStatuses) && httpHealthStatuses[httpServerEventCount] {
				writer.WriteHeader(http.StatusOK)
				writer.Write([]byte("foobar"))
			} else {
				writer.WriteHeader(http.StatusInternalServerError)
			}
			httpServerEventCount++
		}))
		host, ports, err := net.SplitHostPort(strings.TrimPrefix(sss.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(ports)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(sss.Close)
		httpHealthChecker := NewWorkloadHealthChecker(&v1alpha3.ReadinessProbe{
			InitialDelaySeconds: 0,
			TimeoutSeconds:      1,
			PeriodSeconds:       1,
			SuccessThreshold:    1,
			FailureThreshold:    1,
			HealthCheckMethod: &v1alpha3.ReadinessProbe_HttpGet{
				HttpGet: &v1alpha3.HTTPHealthCheckConfig{
					Path:   httpPath,
					Port:   uint32(port),
					Scheme: "http",
					Host:   host,
				},
			},
		}, nil, []string{"127.0.0.1"}, false)
		// Speed up tests
		httpHealthChecker.config.CheckFrequency = time.Millisecond
		quitChan := test.NewStop(t)
		expectedHTTPEvents := [4]*ProbeEvent{
			{Healthy: true},
			{Healthy: false},
			{Healthy: true},
			{Healthy: false},
		}

		eventNum := atomic.NewInt32(0)
		go httpHealthChecker.PerformApplicationHealthCheck(func(event *ProbeEvent) {
			if event.Healthy != expectedHTTPEvents[eventNum.Load()].Healthy {
				t.Errorf("tcp: got event healthy: %v at idx %v when expected healthy: %v",
					event.Healthy, eventNum.Load(), expectedHTTPEvents[eventNum.Load()].Healthy)
			}
			eventNum.Inc()
		}, quitChan)

		retry.UntilSuccessOrFail(t, func() error {
			if int(eventNum.Load()) != len(expectedHTTPEvents) {
				return fmt.Errorf("waiting for %v events", len(expectedHTTPEvents)-int(eventNum.Load()))
			}
			return nil
		}, retry.Delay(time.Millisecond*10), retry.Timeout(time.Second))
	})
}
