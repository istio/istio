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
	// totalPushCounts records total number of SDS pushes since server starts serving.
	totalPushCounts = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "total_pushes",
		Help:      "The total number of SDS pushes.",
	})

	// totalPushErrorCounts records total number of failed SDS pushes since server starts serving.
	totalPushErrorCounts = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "total_push_errors",
		Help:      "The total number of failed SDS pushes.",
	})

	// totalActiveConnCounts records total number of active SDS connections.
	totalActiveConnCounts = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "total_active_connections",
		Help:      "The total number of active SDS connections.",
	})

	// totalStaleConnCounts records total number of stale SDS connections.
	totalStaleConnCounts = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "total_stale_connections",
		Help:      "The total number of stale SDS connections.",
	})

	// pendingPushPerConnCounts records the number of SDS requests in an active connection that are
	// not responded yet. The label of a connection is represented as <resource name>-<connection ID>,
	// and the value should be 0 or 1.
	pendingPushPerConnCounts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "pending_push_per_connection",
		Help:      "The number of active SDS connections which are waiting for SDS push.",
	}, []string{"resourcePerConn"})

	// staleConnCounts records all the stale connections which will be closed. The label of a
	// stale connection is represented as <connection ID>, and the value should be 1.
	staleConnCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "stale_conn_count",
		Help:      "The number of stale SDS connections.",
	}, []string{"resourcePerConn"})

	// pushPerConnCounts records the number of SDS pushes in an active connection. The label of a
	// connection is represented as <resource name>-<connection ID>, and the value should be at
	// least 1.
	pushPerConnCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "pushes_per_connection",
		Help:      "The number of secret pushes to an active SDS connection.",
	}, []string{"resourcePerConn"})

	// pushErrorsPerConnCounts records the number of SDS push failures in an active connection.
	// The label of a connection is represented as <resource name>-<connection ID>, and the value
	// should be at least 1.
	pushErrorsPerConnCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel_agent",
		Subsystem: "sds_service",
		Name:      "push_errors_per_connection",
		Help:      "The number of failed secret pushes to an active SDS connection.",
	}, []string{"resourcePerConn"})

	// rootCertExpiryTimestamp records the expiration timestamp of the most recent pushed root
	// certificate for a particular SDS resource. The label of a pushed root cert is represented as
	// <resource name>-<connection ID>, and the value is in Unix Epoch Time.
	rootCertExpiryTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "citadel_agent",
			Name:      "pushed_root_cert_expiry_timestamp",
			Subsystem: "sds_service",
			Help: "The date after which a pushed root certificate expires. Expressed as a Unix Epoch Time.",
		}, []string{"resourcePerConn"})

	// serverCertExpiryTimestamp records the expiration timestamp of the most recent pushed server
	// certificate for a particular SDS resource. The label of a pushed root cert is represented as
	// <resource name>-<connection ID>, and the value is in Unix Epoch Time.
	serverCertExpiryTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "citadel_agent",
			Name:      "pushed_server_cert_expiry_timestamp",
			Subsystem: "sds_service",
			Help: "The date after which a pushed server certificate expires. Expressed as a Unix Epoch Time.",
		}, []string{"resourcePerConn"})
)

func init() {
	prometheus.MustRegister(totalPushCounts)
	prometheus.MustRegister(totalPushErrorCounts)
	prometheus.MustRegister(totalActiveConnCounts)
	prometheus.MustRegister(totalStaleConnCounts)
	prometheus.MustRegister(pendingPushPerConnCounts)
	prometheus.MustRegister(staleConnCounts)
	prometheus.MustRegister(pushPerConnCounts)
	prometheus.MustRegister(rootCertExpiryTimestamp)
	prometheus.MustRegister(serverCertExpiryTimestamp)
}

// monitoringMetrics are counters for SDS push related operations.
type monitoringMetrics struct {
	totalPush                 prometheus.Counter
	totalPushError            prometheus.Counter
	totalActiveConn           prometheus.Gauge
	totalStaleConn            prometheus.Gauge
	pendingPushPerConn        *prometheus.GaugeVec
	staleConn                 *prometheus.CounterVec
	pushPerConn               *prometheus.CounterVec
	pushErrorPerConn          *prometheus.CounterVec
	rootCertExpiryTimestamp   *prometheus.GaugeVec
	serverCertExpiryTimestamp *prometheus.GaugeVec
}

// newMonitoringMetrics creates a new monitoringMetrics.
func newMonitoringMetrics() monitoringMetrics {
	return monitoringMetrics{
		totalPush:                 totalPushCounts,
		totalPushError:            totalPushErrorCounts,
		totalActiveConn:           totalActiveConnCounts,
		totalStaleConn:            totalStaleConnCounts,
		pendingPushPerConn:        pendingPushPerConnCounts,
		staleConn:                 staleConnCounts,
		pushPerConn:               pushPerConnCounts,
		pushErrorPerConn:          pushErrorsPerConnCounts,
		rootCertExpiryTimestamp:   rootCertExpiryTimestamp,
		serverCertExpiryTimestamp: serverCertExpiryTimestamp,
	}
}
