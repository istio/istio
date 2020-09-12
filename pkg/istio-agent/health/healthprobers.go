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
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/pkg/log"
	"net/http"
	"net/url"
	"time"
)

var healthCheckLog = log.RegisterScope("healthcheck", "Health Checks performed by Istio-Agent", 0)


type Prober interface {
	// Probe will healthcheck and return whether or not the target is healthy.
	Probe() (bool, error)
}

type HTTPProber struct {
	config v1alpha3.ReadinessProbe_HttpGet
}

// HttpProber_Probe will return whether or not the target is healthy (true -> healthy).
func (h *HTTPProber) Probe(timeout time.Duration) (bool, error) {
	client := &http.Client{
		Timeout: timeout,
	}
	if h.config.HttpGet.Scheme == string(scheme.HTTP) {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	headers := make(http.Header)
	for _, val := range h.config.HttpGet.HttpHeaders {
		headers[val.Name] = []string{val.Value}
	}
	targetURL, err := url.Parse(fmt.Sprintf("%s://%s:%v/%s", h.config.HttpGet.Scheme, h.config.HttpGet.Host,
		h.config.HttpGet.Port, h.config.HttpGet.Path))
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
	defer res.Body.Close()
	if res.StatusCode > http.StatusOK && res.StatusCode < http.StatusBadRequest {
		healthCheckLog.Infof("Health check succeeded for %v", targetURL.String())
		return true, nil
	}
	return false, nil
}

type TCPProber struct {
	config v1alpha3.ReadinessProbe_TcpSocket
}

func (t *TCPProber) Probe() (bool, error) {
	return false, nil
}

type ExecProber struct {
	config v1alpha3.ReadinessProbe_Exec
}

func (e *ExecProber) Probe() (bool, error) {
	return false, nil
}
