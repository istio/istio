// Copyright 2019 Istio Authors
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

package sds

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	totalPushCounts = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "total_push_count",
		Help:      "The total number of SDS pushes.",
	})

	pendingPushPerConnCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "push_count",
		Help:      "The number of secret pushes to an active SDS connection.",
	}, []string{"resourcePerConn"})

	pushPerConnCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "push_count",
		Help:      "The number of secret pushes to an active SDS connection.",
	}, []string{"resourcePerConn"})

	pushFailurePerConnCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "push_failure_count",
		Help:      "The number of failed secret pushes to an active SDS connection.",
	}, []string{"resourcePerConn"})

	rootCertExpiryTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "citadel_agent",
			Name:      "pushed_root_cert_expiry_timestamp",
			Subsystem: "sds_service",
			Help: "The unix timestamp, in seconds, when pushed root cert will expire. " +
				"We set it to negative in case of internal error.",
		}, []string{"resource"})

	serverCertExpiryTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "citadel_agent",
			Name:      "pushed_server_cert_expiry_timestamp",
			Subsystem: "sds_service",
			Help: "The unix timestamp, in seconds, when pushed server cert will expire. " +
				"We set it to negative in case of internal error.",
		}, []string{"resource"})
)

func init() {
	prometheus.MustRegister(totalPushCounts)
	prometheus.MustRegister(pendingPushPerConnCounts)
	prometheus.MustRegister(pushPerConnCounts)
	prometheus.MustRegister(rootCertExpiryTimestamp)
	prometheus.MustRegister(serverCertExpiryTimestamp)
}

// monitoringMetrics are counters for SDS push related operations.
type monitoringMetrics struct {
	totalPush                 prometheus.Counter
	pendingPushPerConn        *prometheus.CounterVec
	pushPerConn               *prometheus.CounterVec
	pushFailurePerConn        *prometheus.CounterVec
	rootCertExpiryTimestamp   *prometheus.GaugeVec
	serverCertExpiryTimestamp *prometheus.GaugeVec
}

// newMonitoringMetrics creates a new monitoringMetrics.
func newMonitoringMetrics() monitoringMetrics {
	return monitoringMetrics{
		totalPush:                 totalPushCounts,
		pendingPushPerConn:        pendingPushPerConnCounts,
		pushPerConn:               pushPerConnCounts,
		pushFailurePerConn:        pushFailurePerConnCounts,
		rootCertExpiryTimestamp:   rootCertExpiryTimestamp,
		serverCertExpiryTimestamp: serverCertExpiryTimestamp,
	}
}
