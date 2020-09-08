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
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/genproto/googleapis/rpc/status"
)

const (
	HealthInfoTypeURL string = "type.googleapis.com/istio.v1.HealthInformation"
)

type WorkloadHealthChecker struct {
	config ApplicationHealthCheckConfig
	prober Prober
}

type ApplicationHealthCheckConfig struct {
	InitialDelay   time.Duration
	ProbeTimeout   time.Duration
	CheckFrequency time.Duration
	SuccessThresh  int
	FailThresh     int
}

func NewWorkloadHealthChecker(prober Prober, cfg ApplicationHealthCheckConfig) *WorkloadHealthChecker {

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
	for {
		// probe target
		healthy, err := w.prober.Probe()
		if err != nil {
			// todo handle error
		}
		if healthy {
			// we were healthy, increment success counter
			numSuccess++
			// if we reached the threshold, mark the target as healthy
			if numSuccess == w.config.SuccessThresh && !lastStateHealthy {
				notifyHealthChange <- &discovery.DiscoveryRequest{TypeUrl: HealthInfoTypeURL}
				numSuccess = 0
				numFail = 0
				lastStateHealthy = true
			}
		} else {
			// we were not healthy, increment fail counter
			numFail++
			// if we reached the fail threshold, mark the target as unhealthy
			if numFail == w.config.FailThresh && lastStateHealthy {
				notifyHealthChange <- &discovery.DiscoveryRequest{
					TypeUrl: HealthInfoTypeURL,
					ErrorDetail: &status.Status{
						Code:    int32(500),
						Message: "unhealthy",
					},
				}
				numSuccess = 0
				numFail = 0
				lastStateHealthy = false
			}
		}
	}
}

// TODO implement
func (w *WorkloadHealthChecker) PerformEnvoyHealthCheck() {

}
