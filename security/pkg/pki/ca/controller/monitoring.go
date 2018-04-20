// Copyright 2018 Istio Authors
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

package controller

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	serviceAccountCreationCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "svc_acc_created_cert_count",
		Help:      "The number of certificates created due to service account creation.",
	}, []string{})

	serviceAccountDeletionCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "svc_acc_deleted_cert_count",
		Help:      "The number of certificates deleted due to service account deletion.",
	}, []string{})

	secretDeletionCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "secret_deleted_cert_count",
		Help:      "The number of certificates recreated due to secret deletion (service account still exists).",
	}, []string{})
)

func init() {
	prometheus.MustRegister(serviceAccountCreationCount)
	prometheus.MustRegister(serviceAccountDeletionCount)
	prometheus.MustRegister(secretDeletionCount)
}

// monitoringMetrics are counters for secret controller operations.
type monitoringMetrics struct {
	ServiceAccountCreation prometheus.Counter
	ServiceAccountDeletion prometheus.Counter
	SecretDeletion         prometheus.Counter
}

// newMonitoringMetrics creates a new monitoringMetrics.
func newMonitoringMetrics() monitoringMetrics {
	labels := prometheus.Labels{}
	return monitoringMetrics{
		ServiceAccountCreation: serviceAccountCreationCount.With(labels),
		ServiceAccountDeletion: serviceAccountDeletionCount.With(labels),
		SecretDeletion:         secretDeletionCount.With(labels),
	}
}
