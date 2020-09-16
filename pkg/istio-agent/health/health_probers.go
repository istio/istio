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
	"strconv"
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
	Probe(timeout time.Duration) (ProbeResult, error)
}

type ProbeResult string

const (
	Healthy   ProbeResult = "HEALTHY"
	Unhealthy ProbeResult = "UNHEALTHY"
	Unknown   ProbeResult = "UNKNOWN"
)

func (p *ProbeResult) IsHealthy() bool {
	return *p == Healthy
}

func (p *ProbeResult) IsUnhealthy() bool {
	return *p == Unhealthy
}

func (p *ProbeResult) IsUnknown() bool {
	return *p == Unknown
}

type HTTPProber struct {
	Config *v1alpha3.HTTPHealthCheckConfig
}

// HttpProber_Probe will return whether or not the target is healthy (true -> healthy)
// 	by making an HTTP Get response.
func (h *HTTPProber) Probe(timeout time.Duration) (ProbeResult, error) {
	client := &http.Client{
		Timeout: timeout,
	}
	// modify transport if scheme is https
	if h.Config.Scheme == string(scheme.HTTPS) {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	// transform crd into net http header
	headers := make(http.Header)
	for _, val := range h.Config.HttpHeaders {
		// net.httpHeaders value is a []string but uses only index 0
		headers[val.Name] = append(headers[val.Name], val.Value)
	}
	targetURL, err := url.Parse(h.Config.Path)
	// Something is busted with the path, but it's too late to reject it. Pass it along as is.
	if err != nil {
		targetURL = &url.URL{
			Path: h.Config.Path,
		}
	}
	targetURL.Scheme = h.Config.Scheme
	targetURL.Host = net.JoinHostPort(h.Config.Host, strconv.Itoa(int(h.Config.Port)))
	if err != nil {
		healthCheckLog.Errorf("unable to parse url: %v", err)
		return Unknown, err
	}
	req, err := http.NewRequest("GET", targetURL.String(), nil)
	if err != nil {
		return Unknown, err
	}
	req.Header = headers
	if headers.Get("Host") != "" {
		req.Host = headers.Get("Host")
	}
	res, err := client.Do(req)
	// if we were unable to connect, count as failure
	if err != nil {
		healthCheckLog.Infof("Health Check failed for %v: %v", targetURL.String(), err)
		return Unhealthy, err
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
		return Healthy, nil
	}
	return Unhealthy, fmt.Errorf("status code was not from [200,400), bad code %v", res.StatusCode)
}

type TCPProber struct {
	Config *v1alpha3.TCPHealthCheckConfig
}

func (t *TCPProber) Probe(timeout time.Duration) (ProbeResult, error) {
	// if we cant connect, count as fail
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%v", t.Config.Host, t.Config.Port), timeout)
	if err != nil {
		return Unhealthy, err
	}
	err = conn.Close()
	if err != nil {
		healthCheckLog.Errorf("Unable to close TCP Socket: %v", err)
	}
	return Healthy, nil
}

type ExecProber struct {
	Config *v1alpha3.ExecHealthCheckConfig
}

func (e *ExecProber) Probe(timeout time.Duration) (ProbeResult, error) {
	cmd := exec.Cmd{
		Path: e.Config.Command[0],
		Args: e.Config.Command[1:],
	}
	if err := cmd.Start(); err != nil {
		// should this be unknown? exit code returns status, this shouldnt
		// should we extract exit status from here?
		return Unhealthy, err
	}

	// wait on another channel
	done := make(chan error)
	go func() { done <- cmd.Wait() }()
	// start timeout timer
	timeoutTimer := time.After(timeout)

	select {
	case <-timeoutTimer:
		if err := cmd.Process.Kill(); err != nil {
			healthCheckLog.Errorf("Unable to kill process after timeout: %v", err)
			return Unhealthy, err
		}
		// timeout exceeded counts as unhealthy, return nil err
		return Unhealthy, nil
	case err := <-done:
		// extract exit status, log and return
		if err == nil {
			return Healthy, nil
		}
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.ExitCode() == 0 {
				// exited successfully
				return Healthy, nil
			}
			healthCheckLog.Infof("Command %v exited with non-zero status %v", cmd.String(), exitError.ExitCode())
			return Unhealthy, err
		}
		return Unhealthy, fmt.Errorf("could not extract ExitError from command error")
	}
}
