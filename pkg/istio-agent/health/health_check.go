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
	"strings"
	"time"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pkg/kube/apimirror"
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

const (
	lastStateUndefined = iota
	lastStateHealthy
	lastStateUnhealthy
)

func fillInDefaults(cfg *v1alpha3.ReadinessProbe) *v1alpha3.ReadinessProbe {
	cfg = cfg.DeepCopy()
	// Thresholds have a minimum of 1
	cfg.FailureThreshold = orDefault(cfg.FailureThreshold, 1)
	cfg.SuccessThreshold = orDefault(cfg.SuccessThreshold, 1)

	// InitialDelaySeconds allows zero, no default needed
	cfg.TimeoutSeconds = orDefault(cfg.TimeoutSeconds, 1)
	cfg.PeriodSeconds = orDefault(cfg.PeriodSeconds, 10)

	switch h := cfg.HealthCheckMethod.(type) {
	case *v1alpha3.ReadinessProbe_HttpGet:
		if h.HttpGet.Path == "" {
			h.HttpGet.Path = "/"
		}
		if h.HttpGet.Scheme == "" {
			h.HttpGet.Scheme = string(apimirror.URISchemeHTTP)
		}
		h.HttpGet.Scheme = strings.ToLower(h.HttpGet.Scheme)
		if h.HttpGet.Host == "" {
			// Kubernetes uses pod IP. However, the istio rewrite app probe uses localhost, so we
			// should probably favor consistency with Istio than Kubernetes
			h.HttpGet.Host = "localhost"
		}
	}
	return cfg
}

func NewWorkloadHealthChecker(cfg *v1alpha3.ReadinessProbe, envoyProbe ready.Prober) *WorkloadHealthChecker {
	// if a config does not exist return a no-op prober
	if cfg == nil {
		return nil
	}
	cfg = fillInDefaults(cfg)
	var prober Prober
	switch healthCheckMethod := cfg.HealthCheckMethod.(type) {
	case *v1alpha3.ReadinessProbe_HttpGet:
		prober = NewHTTPProber(healthCheckMethod.HttpGet)
	case *v1alpha3.ReadinessProbe_TcpSocket:
		prober = &TCPProber{Config: healthCheckMethod.TcpSocket}
	case *v1alpha3.ReadinessProbe_Exec:
		prober = &ExecProber{Config: healthCheckMethod.Exec}
	default:
		prober = nil
	}

	probers := []Prober{}
	if envoyProbe != nil {
		probers = append(probers, &EnvoyProber{envoyProbe})
	}
	probers = append(probers, prober)
	return &WorkloadHealthChecker{
		config: applicationHealthCheckConfig{
			InitialDelay:   time.Duration(cfg.InitialDelaySeconds) * time.Second,
			ProbeTimeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
			CheckFrequency: time.Duration(cfg.PeriodSeconds) * time.Second,
			SuccessThresh:  int(cfg.SuccessThreshold),
			FailThresh:     int(cfg.FailureThreshold),
		},
		prober: AggregateProber{Probes: probers},
	}
}

func orDefault(val int32, def int32) int32 {
	if val == 0 {
		return def
	}
	return val
}

// PerformApplicationHealthCheck Performs the application-provided configuration health check.
// Instead of a heartbeat-based health checks, we only send on a health state change, and this is
// determined by the success & failure threshold provided by the user.
func (w *WorkloadHealthChecker) PerformApplicationHealthCheck(callback func(*ProbeEvent), quit chan struct{}) {
	if w == nil {
		return
	}

	healthCheckLog.Infof("starting health check for %T in %v", w.prober, w.config.InitialDelay)
	// delay before starting probes.
	time.Sleep(w.config.InitialDelay)

	// tracks number of success & failures after last success/failure
	numSuccess, numFail := 0, 0
	// if the last send/event was a success, this is true, by default false because we want to
	// first send a healthy message.
	lastState := lastStateUndefined

	doCheck := func() {
		// probe target
		healthy, err := w.prober.Probe(w.config.ProbeTimeout)
		if healthy.IsHealthy() {
			healthCheckLog.Debug("probe completed with healthy status")
			// we were healthy, increment success counter
			numSuccess++
			// wipe numFail (need consecutive success)
			numFail = 0
			// if we reached the threshold, mark the target as healthy
			if numSuccess == w.config.SuccessThresh && lastState != lastStateHealthy {
				healthCheckLog.Info("success threshold hit, marking as healthy")
				callback(&ProbeEvent{Healthy: true})
				numSuccess = 0
				lastState = lastStateHealthy
			}
		} else {
			healthCheckLog.Debugf("probe completed with unhealthy status: %v", err)
			// we were not healthy, increment fail counter
			numFail++
			// wipe numSuccess (need consecutive failure)
			numSuccess = 0
			// if we reached the fail threshold, mark the target as unhealthy
			if numFail == w.config.FailThresh && lastState != lastStateUnhealthy {
				healthCheckLog.Infof("failure threshold hit, marking as unhealthy: %v", err)
				numFail = 0
				callback(&ProbeEvent{
					Healthy:          false,
					UnhealthyStatus:  500,
					UnhealthyMessage: err.Error(),
				})
				lastState = lastStateUnhealthy
			}
		}
	}

	// Send the first request immediately
	doCheck()
	periodTicker := time.NewTicker(w.config.CheckFrequency)
	for {
		select {
		case <-quit:
			return
		case <-periodTicker.C:
			doCheck()
		}
	}
}
