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

package istioagent

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/genproto/googleapis/rpc/status"
)

type HealthCheckType string

const (
	HTTPHealthCheck   HealthCheckType = "HTTP"
	TCPHealthCheck    HealthCheckType = "TCP"
	ExecHealthCheck   HealthCheckType = "Exec"
	HealthInfoTypeURL string          = "type.googleapis.com/istio.v1.HealthInformation"
)

type WorkloadHealthChecker struct {
	config ApplicationHealthCheckConfig
}

type ApplicationHealthCheckConfig struct {
	InitialDelay   time.Duration
	ProbeTimeout   time.Duration
	CheckFrequency time.Duration
	SuccessThresh  int
	FailThresh     int
	CheckType      HealthCheckType
	HTTPConfig     *HTTPHealthCheckConfig
	TCPConfig      *TCPHealthCheckConfig
	ExecConfig     *ExecHealthCheckConfig
}

type HTTPHealthCheckConfig struct {
	Path    string
	Port    uint32
	Scheme  string
	Headers map[string][]string
}

type TCPHealthCheckConfig struct {
	Host string
	Port string
}

type ExecHealthCheckConfig struct {
	ExecutableName string
	Args           []string
}

// PerformApplicationHealthCheck Performs the application-provided configuration health check.
// Designed to run async.
// TODO:
// 	- Add channel param for quit (better error handling as well)
// 	- Because there are 3 possible configs, there are 3 possible healthcheck paths, and all of them
// 		are defined here. Therefore, there is quite a bit of duplicate code in success/fail threshold
// 		and healthChannel sending. This code should be better.
// 	- Should the CheckFrequency Delay be a time.Ticker?
func (w *WorkloadHealthChecker) PerformApplicationHealthCheck(notifyHealthChange chan *discovery.DiscoveryRequest) {
	// delay before starting probes.
	time.Sleep(w.config.InitialDelay)

	// tracks number of success & failures after last success/failure
	numSuccess, numFail := 0, 0
	// if the last send/event was a success, this is true, by default false because we want to
	// first send a healthy message.
	lastStateHealthy := false
	if w.config.CheckType == HTTPHealthCheck {
		for {
			// healthy
			if code, err := httpCheck(*w.config.HTTPConfig, w.config.ProbeTimeout); code >= 200 && code <= 299 {
				numSuccess++
				if numSuccess == w.config.SuccessThresh && !lastStateHealthy {
					notifyHealthChange <- &discovery.DiscoveryRequest{TypeUrl: HealthInfoTypeURL}
					numSuccess = 0
					numFail = 0
					lastStateHealthy = true
				}
			} else {
				// not healthy
				numFail++
				// application is officially unhealthy if the number of failures matches
				// the failure threshold set by the user and the last event was application healthy.
				if numFail == w.config.FailThresh && lastStateHealthy {
					notifyHealthChange <- &discovery.DiscoveryRequest{
						TypeUrl: HealthInfoTypeURL,
						ErrorDetail: &status.Status{
							Code:    int32(code),
							Message: err.Error(),
						},
					}
					numSuccess = 0
					numFail = 0
					lastStateHealthy = false
				}
			}
			// should this be a time.Ticker?
			time.Sleep(w.config.CheckFrequency)
		}
	}

	if w.config.CheckType == TCPHealthCheck {
		for {
			if err := tcpCheck(*w.config.TCPConfig, w.config.ProbeTimeout); err == nil {
				numSuccess++
				if numSuccess == w.config.SuccessThresh && !lastStateHealthy {
					notifyHealthChange <- &discovery.DiscoveryRequest{TypeUrl: HealthInfoTypeURL}
					numSuccess = 0
					numFail = 0
					lastStateHealthy = true
				}
			} else {
				numFail++
				if numFail == w.config.FailThresh && lastStateHealthy {
					notifyHealthChange <- &discovery.DiscoveryRequest{
						TypeUrl: HealthInfoTypeURL,
						ErrorDetail: &status.Status{
							Code:    int32(500),
							Message: err.Error(),
						},
					}
					numSuccess = 0
					numFail = 0
					lastStateHealthy = false
				}
			}
		}
	}

	if w.config.CheckType == ExecHealthCheck {
		for {
			if err := execCheck(*w.config.ExecConfig); err == nil {
				numSuccess++
				if numSuccess == w.config.SuccessThresh && !lastStateHealthy {
					notifyHealthChange <- &discovery.DiscoveryRequest{TypeUrl: HealthInfoTypeURL}
					numSuccess = 0
					numFail = 0
					lastStateHealthy = false
				} else {
					numFail++
					if numFail == w.config.FailThresh && lastStateHealthy {
						notifyHealthChange <- &discovery.DiscoveryRequest{
							TypeUrl: HealthInfoTypeURL,
							ErrorDetail: &status.Status{
								Code:    int32(500),
								Message: err.Error(),
							},
						}
						numSuccess = 0
						numFail = 0
						lastStateHealthy = false
					}
				}
			}
		}
	}
}

// httpCheck performs a http get to a given endpoint with a timeout, and returns
// the status and error.
func httpCheck(config HTTPHealthCheckConfig, timeout time.Duration) (int, error) {
	client := http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	checkURL, err := url.Parse(fmt.Sprintf("%s://localhost:%v/%s", config.Scheme, config.Port, config.Path))
	req := &http.Request{
		Method: "GET",
		URL:    checkURL,
		Header: config.Headers,
	}
	resp, err := client.Do(req)
	if err != nil {
		resp.Body.Close()
	}
	return resp.StatusCode, err
}

// tcpCheck connects to a given endpoint with a timeout, and errors
// if connecting is unavailable (unhealthy)
func tcpCheck(config TCPHealthCheckConfig, timeout time.Duration) error {
	d := net.Dialer{
		Timeout: timeout,
	}
	url := fmt.Sprintf("%s:%v", config.Host, config.Port)
	conn, err := d.Dial("tcp", url)
	if conn != nil {
		conn.Close()
	}
	return err
}

// todo does adding Stderr to exec cmd return the error there?
func execCheck(config ExecHealthCheckConfig) error {
	healthCheckCmd := &exec.Cmd{
		Path:   config.ExecutableName,
		Args:   config.Args,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
	}
	return healthCheckCmd.Run()
}

// TODO implement
func (w *WorkloadHealthChecker) PerformEnvoyHealthCheck() {

}
