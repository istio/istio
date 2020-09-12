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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"time"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/pkg/log"
)

var healthCheckLog = log.RegisterScope("healthcheck", "Health Checks performed by Istio-Agent", 0)

type Prober interface {
	// Probe will healthcheck and return whether or not the target is healthy.
	// If an error returned is not nil, it is assumed that the process could
	// not complete, and Probe() was unable to determine whether or not the
	// target was healthy.
	Probe(timeout time.Duration) (bool, error)
}

type HTTPProber struct {
	Config *v1alpha3.HTTPHealthCheckConfig
}

// HttpProber_Probe will return whether or not the target is healthy (true -> healthy)
// 	by making an HTTP Get response.
func (h *HTTPProber) Probe(timeout time.Duration) (bool, error) {
	client := &http.Client{
		Timeout: timeout,
	}
	// modify transport if scheme is http
	if h.Config.Scheme == string(scheme.HTTP) {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	// transform crd into net http header
	headers := make(http.Header)
	for _, val := range h.Config.HttpHeaders {
		// net.httpHeaders value is a []string but uses only index 0
		headers[val.Name] = []string{val.Value}
	}
	// GET scheme://ip:port/path
	targetURL, err := url.Parse(fmt.Sprintf("%s://localhost:%v/%s", h.Config.Scheme, h.Config.Port, h.Config.Path))
	if err != nil {
		healthCheckLog.Errorf("unable to parse url: %v", err)
		return false, err
	}
	req, err := http.NewRequest("GET", targetURL.String(), nil)
	if err != nil {
		return false, err
	}
	req.Header = headers
	if headers.Get("Host") != "" {
		req.Host = headers.Get("Host")
	}
	res, err := client.Do(req)
	// if we were unable to connect, count as failure
	if err != nil {
		healthCheckLog.Infof("Health Check failed for %v: %v", targetURL.String(), err)
		return false, nil
	}
	defer func() {
		err = res.Body.Close()
		if err != nil {
			healthCheckLog.Error(err)
		}
	}()
	// from [200,400)
	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusBadRequest {
		healthCheckLog.Infof("Health check succeeded for %v", targetURL.String())
		return true, nil
	}
	return false, nil
}

type TCPProber struct {
	Config *v1alpha3.TCPHealthCheckConfig
}

func (t *TCPProber) Probe(timeout time.Duration) (bool, error) {
	// if we cant connect, count as fail
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%v", t.Config.Host, t.Config.Port), timeout)
	if err != nil {
		return false, nil
	}
	err = conn.Close()
	if err != nil {
		healthCheckLog.Errorf("Unable to close TCP Socket: %v", err)
	}
	return true, nil
}

type ExecProber struct {
	Config *v1alpha3.ExecHealthCheckConfig
}

func (e *ExecProber) Probe(timeout time.Duration) (bool, error) {
	cmd := exec.Cmd{
		Path: e.Config.Command[0],
		Args: e.Config.Command[1:],
	}
	_ = cmd.Start()

	// wait on another channel
	done := make(chan error)
	go func() { done <- cmd.Wait() }()
	// start timeout timer
	timeoutTimer := time.After(timeout)

	select {
	case <-timeoutTimer:
		// timeout exceeded counts as unhealthy, return nil err
		return false, nil
	case err := <-done:
		// extract exit status, log and return
		if err == nil {
			return true, nil
		}
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.ExitCode() == 0 {
				// exited successfully
				return true, nil
			}
			healthCheckLog.Infof("Command %v exited with non-zero status %v", cmd.String(), exitError.ExitCode())
			return false, nil
		}
		return false, fmt.Errorf("could not extract ExitError from command error")
	}
}

type NoOpProber struct{}

func (n *NoOpProber) Probe(_ time.Duration) (bool, error) {
	return true, nil
}
