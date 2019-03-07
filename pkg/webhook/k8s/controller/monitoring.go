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

const (
	errorlabel = "error"
)

var (
	serviceAccountCreationCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "svc_acc_created_cert_count",
		Help:      "The number of certificates created due to service account creation.",
	}, []string{})

	serviceAccountDeletionCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "svc_acc_deleted_cert_count",
		Help:      "The number of certificates deleted due to service account deletion.",
	}, []string{})

	secretDeletionCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "secret_deleted_cert_count",
		Help:      "The number of certificates recreated due to secret deletion (service account still exists).",
	}, []string{})

	csrErrorCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "csr_err_count",
		Help:      "The number of errors occurred when creating the CSR.",
	}, []string{})

	certSignErrorCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "csr_sign_err_count",
		Help:      "The number of erorrs occurred when signing the CSR.",
	}, []string{errorlabel})
)

func init() {
	prometheus.MustRegister(serviceAccountCreationCounts)
	prometheus.MustRegister(serviceAccountDeletionCounts)
	prometheus.MustRegister(secretDeletionCounts)
	prometheus.MustRegister(csrErrorCounts)
	prometheus.MustRegister(certSignErrorCounts)
}

// monitoringMetrics are counters for secret controller operations.
type monitoringMetrics struct {
	ServiceAccountCreation prometheus.Counter
	ServiceAccountDeletion prometheus.Counter
	SecretDeletion         prometheus.Counter
	CSRError               prometheus.Counter
	certSignErrors         *prometheus.CounterVec
}

// newMonitoringMetrics creates a new monitoringMetrics.
func newMonitoringMetrics() monitoringMetrics {
	return monitoringMetrics{
		ServiceAccountCreation: serviceAccountCreationCounts.With(prometheus.Labels{}),
		ServiceAccountDeletion: serviceAccountDeletionCounts.With(prometheus.Labels{}),
		SecretDeletion:         secretDeletionCounts.With(prometheus.Labels{}),
		CSRError:               csrErrorCounts.With(prometheus.Labels{}),
		certSignErrors:         certSignErrorCounts,
	}
}

func (m *monitoringMetrics) GetCertSignError(err string) prometheus.Counter {
	return m.certSignErrors.With(prometheus.Labels{errorlabel: err})
}
