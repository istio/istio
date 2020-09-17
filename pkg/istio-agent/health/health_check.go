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
	"time"

	"istio.io/api/networking/v1alpha3"
)

const (
	HealthInfoTypeURL string = "type.googleapis.com/istio.v1.HealthInformation"
)

type WorkloadHealthChecker struct {
	config applicationHealthCheckConfig
	prober Prober
}

// internal field purely for convenience
type applicationHealthCheckConfig struct {
	InitialDelay   time.Duration
	ProbeTimeout   time.Duration
	CheckFrequency time.Duration
	SuccessThresh  int
	FailThresh     int
}

type ProbeEvent struct {
	Healthy          bool
	UnhealthyStatus  int32
	UnhealthyMessage string
}

func NewWorkloadHealthChecker(cfg *v1alpha3.ReadinessProbe) *WorkloadHealthChecker {
	// if a config does not exist return a no-op prober
	if cfg == nil {
		return &WorkloadHealthChecker{
			config: applicationHealthCheckConfig{},
			prober: nil,
		}
	}
	var prober Prober
	switch healthCheckMethod := cfg.HealthCheckMethod.(type) {
	case *v1alpha3.ReadinessProbe_HttpGet:
		prober = &HTTPProber{Config: healthCheckMethod.HttpGet}
	case *v1alpha3.ReadinessProbe_TcpSocket:
		prober = &TCPProber{Config: healthCheckMethod.TcpSocket}
	case *v1alpha3.ReadinessProbe_Exec:
		prober = &ExecProber{Config: healthCheckMethod.Exec}
	default:
		prober = nil
	}

	return &WorkloadHealthChecker{
		config: applicationHealthCheckConfig{
			InitialDelay:   time.Duration(cfg.InitialDelaySeconds) * time.Second,
			ProbeTimeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
			CheckFrequency: time.Duration(cfg.PeriodSeconds) * time.Second,
			SuccessThresh:  int(cfg.SuccessThreshold),
			FailThresh:     int(cfg.FailureThreshold),
		},
		prober: prober,
	}
}

// PerformApplicationHealthCheck Performs the application-provided configuration health check.
// Instead of a heartbeat-based health checks, we only send on a health state change, and this is
// determined by the success & failure threshold provided by the user.
func (w *WorkloadHealthChecker) PerformApplicationHealthCheck(notifyHealthChange chan *ProbeEvent, quit chan struct{}) {
	defer close(notifyHealthChange)

	// no-op
	if w.prober == nil {
		return
	}

	// delay before starting probes.
	time.Sleep(w.config.InitialDelay)

	// tracks number of success & failures after last success/failure
	numSuccess, numFail := 0, 0
	// if the last send/event was a success, this is true, by default false because we want to
	// first send a healthy message.
	lastStateHealthy := false

	if w.config.CheckFrequency == time.Second*0 {
		// should probably hard-code a value somewhere else.
		// like k8s, default to 10s
		w.config.CheckFrequency = time.Second * 10
	}

	periodTicker := time.NewTicker(w.config.CheckFrequency)
	for {
		select {
		case <-quit:
			return
		case <-periodTicker.C:
			// probe target
			healthy, err := w.prober.Probe(w.config.ProbeTimeout)
			if healthy.IsHealthy() {
				// we were healthy, increment success counter
				numSuccess++
				// wipe numFail (need consecutive success)
				numFail = 0
				// if we reached the threshold, mark the target as healthy
				if numSuccess == w.config.SuccessThresh && !lastStateHealthy {
					notifyHealthChange <- &ProbeEvent{Healthy: true}
					numSuccess = 0
					lastStateHealthy = true
				}
			} else {
				// we were not healthy, increment fail counter
				numFail++
				// wipe numSuccess (need consecutive failure)
				numSuccess = 0
				// if we reached the fail threshold, mark the target as unhealthy
				if numFail == w.config.FailThresh && lastStateHealthy {
					notifyHealthChange <- &ProbeEvent{
						Healthy:          false,
						UnhealthyStatus:  500,
						UnhealthyMessage: err.Error(),
					}
					numFail = 0
					lastStateHealthy = false
				}
			}
		}
	}
}

// TODO implement
func (w *WorkloadHealthChecker) PerformEnvoyHealthCheck() {

}
