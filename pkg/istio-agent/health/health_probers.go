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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	grpc_status "google.golang.org/grpc/status"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pkg/kube/apimirror"
	"istio.io/istio/pkg/log"
)

var healthCheckLog = log.RegisterScope("healthcheck", "Health Checks performed by Istio-Agent")

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

type GRPCProber struct {
	Config      *v1alpha3.GrpcHealthCheckConfig
	DefaultHost string
}

var _ Prober = &GRPCProber{}

func NewGRPCProber(cfg *v1alpha3.GrpcHealthCheckConfig, defaultHost string) *GRPCProber {
	return &GRPCProber{
		Config:      cfg,
		DefaultHost: defaultHost,
	}
}

func (g *GRPCProber) Probe(timeout time.Duration) (ProbeResult, error) {
	if g.Config == nil {
		return Unknown, fmt.Errorf("grpc health check config is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, net.JoinHostPort(g.DefaultHost, fmt.Sprintf("%d", g.Config.Port)),
		grpc.WithBlock(),
		grpc.WithUserAgent("istio-probe/1.0"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return status.ProbeDialer().DialContext(ctx, "tcp", addr)
		}),
	)
	if err != nil {
		return Unhealthy, fmt.Errorf("failed to connect health check grpc service on port %d: %v", g.Config.Port, err)
	}
	defer conn.Close()
	client := grpc_health_v1.NewHealthClient(conn)
	req := &grpc_health_v1.HealthCheckRequest{
		Service: g.Config.Service,
	}
	resp, err := client.Check(ctx, req)
	if err != nil {
		if stat, ok := grpc_status.FromError(err); ok {
			switch stat.Code() {
			case codes.Unimplemented:
				return Unhealthy, fmt.Errorf("service on port %d doesn't implement the grpc health protocol grpc.health.v1.Health: %v", g.Config.Port, err)
			case codes.DeadlineExceeded:
				return Unhealthy, fmt.Errorf("health rpc probe timed out after %v: %v", timeout, err)
			default:
				return Unhealthy, fmt.Errorf("health rpc probe failed with status %v: %v", stat.Code(), err)
			}
		} else {
			return Unhealthy, fmt.Errorf("health rpc probe failed: %v", err)
		}
	}
	switch resp.GetStatus() {
	case grpc_health_v1.HealthCheckResponse_SERVING:
		return Healthy, nil
	default:
		return Unhealthy, nil
	}
}

type HTTPProber struct {
	Config      *v1alpha3.HTTPHealthCheckConfig
	Transport   *http.Transport
	DefaultHost string
}

var _ Prober = &HTTPProber{}

func NewHTTPProber(cfg *v1alpha3.HTTPHealthCheckConfig, defaultHost string, ipv6 bool) *HTTPProber {
	h := new(HTTPProber)
	h.Config = cfg

	// Create an http.Transport with TLSClientConfig for HTTPProber if the scheme is https,
	// otherwise set up an empty one.
	if cfg.Scheme == string(apimirror.URISchemeHTTPS) {
		// nolint: gosec
		// This is matching Kubernetes. It is a reasonable usage of this, as it is just a health check over localhost.
		h.Transport = &http.Transport{
			DisableKeepAlives: true,
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		}
	} else {
		h.Transport = &http.Transport{
			DisableKeepAlives: true,
		}
	}
	d := status.ProbeDialer()
	d.LocalAddr = status.UpstreamLocalAddressIPv4
	if ipv6 {
		d.LocalAddr = status.UpstreamLocalAddressIPv6
	}
	h.Transport.DialContext = d.DialContext
	h.DefaultHost = defaultHost
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
	if h.Config.Host == "" {
		targetURL.Host = net.JoinHostPort(h.DefaultHost, strconv.Itoa(int(h.Config.Port)))
	} else {
		targetURL.Host = net.JoinHostPort(h.Config.Host, strconv.Itoa(int(h.Config.Port)))
	}
	if err != nil {
		healthCheckLog.Errorf("unable to parse url: %v", err)
		return Unknown, err
	}
	req, err := http.NewRequest(http.MethodGet, targetURL.String(), nil)
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
	Config      *v1alpha3.TCPHealthCheckConfig
	DefaultHost string
}

func NewTCPProber(cfg *v1alpha3.TCPHealthCheckConfig, host string) *TCPProber {
	return &TCPProber{
		Config:      cfg,
		DefaultHost: host,
	}
}

var _ Prober = &TCPProber{}

func (t *TCPProber) Probe(timeout time.Duration) (ProbeResult, error) {
	// if we can't connect, count as fail
	d := status.ProbeDialer()
	d.Timeout = timeout
	var hostPort string
	if t.Config.Host == "" {
		hostPort = net.JoinHostPort(t.DefaultHost, strconv.Itoa(int(t.Config.Port)))
	} else {
		hostPort = net.JoinHostPort(t.Config.Host, strconv.Itoa(int(t.Config.Port)))
	}
	conn, err := d.Dial("tcp", hostPort)
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
