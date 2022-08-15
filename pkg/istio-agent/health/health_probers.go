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
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"time"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
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

type HTTPProber struct {
	Config    *v1alpha3.HTTPHealthCheckConfig
	Transport *http.Transport
}

var _ Prober = &HTTPProber{}

func NewHTTPProber(cfg *v1alpha3.HTTPHealthCheckConfig, ipv6 bool) *HTTPProber {
	h := new(HTTPProber)
	h.Config = cfg

	// Create an http.Transport with TLSClientConfig for HTTPProber if the scheme is https,
	// otherwise set up an empty one.
	if cfg.Scheme == string(scheme.HTTPS) {
		h.Transport = &http.Transport{
			DisableKeepAlives: true,
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		}
	} else {
		h.Transport = &http.Transport{
			DisableKeepAlives: true,
		}
	}
	d := &net.Dialer{
		LocalAddr: status.UpstreamLocalAddressIPv4,
	}
	if ipv6 {
		d.LocalAddr = status.UpstreamLocalAddressIPv6
	}
	h.Transport.DialContext = d.DialContext
	return h
}

// HttpProber_Probe will return whether or not the target is healthy (true -> healthy)
// by making an HTTP Get response.
func (h *HTTPProber) Probe(timeout time.Duration) (ProbeResult, error) {
	client := &http.Client{
		Timeout:   timeout,
		Transport: h.Transport,
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
	if _, ok := headers["User-Agent"]; !ok {
		// explicitly set User-Agent so it's not set to default Go value. K8s use kube-probe.
		headers.Set("User-Agent", "istio-probe/1.0")
	}
	res, err := client.Do(req)
	// if we were unable to connect, count as failure
	if err != nil {
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
		return Healthy, nil
	}
	return Unhealthy, fmt.Errorf("status code was not from [200,400), bad code %v", res.StatusCode)
}

type TCPProber struct {
	Config *v1alpha3.TCPHealthCheckConfig
}

var _ Prober = &TCPProber{}

func (t *TCPProber) Probe(timeout time.Duration) (ProbeResult, error) {
	// if we cant connect, count as fail
	hostPort := net.JoinHostPort(t.Config.Host, strconv.Itoa(int(t.Config.Port)))
	conn, err := net.DialTimeout("tcp", hostPort, timeout)
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

var _ Prober = &ExecProber{}

func (e *ExecProber) Probe(timeout time.Duration) (ProbeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Config.Command[0], e.Config.Command[1:]...)
	if err := cmd.Run(); err != nil {
		select {
		case <-ctx.Done():
			return Unhealthy, fmt.Errorf("command timeout exceeded: %v", err)
		default:
		}
		return Unhealthy, err
	}
	return Healthy, nil
}

type EnvoyProber struct {
	Config ready.Prober
}

var _ Prober = &EnvoyProber{}

func (a EnvoyProber) Probe(time.Duration) (ProbeResult, error) {
	if err := a.Config.Check(); err != nil {
		return Unhealthy, err
	}
	return Healthy, nil
}

type AggregateProber struct {
	Probes []Prober
}

var _ Prober = &AggregateProber{}

func (a AggregateProber) Probe(timeout time.Duration) (ProbeResult, error) {
	for _, probe := range a.Probes {
		res, err := probe.Probe(timeout)
		if err != nil || !res.IsHealthy() {
			return res, err
		}
	}
	return Healthy, nil
}
