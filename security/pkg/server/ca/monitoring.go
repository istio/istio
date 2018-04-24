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

package ca

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	errorlabel = "error"
)

var (
	csrCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "csr_count",
		Help:      "The number of CSRs recerived by Citadel server.",
	}, []string{})

	authnErrorCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "authentication_failure_count",
		Help:      "The number of authentication failures.",
	}, []string{})

	authzErrorCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "authorization_failure_count",
		Help:      "The number of authorization failures.",
	}, []string{errorlabel})

	certSignErrorCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "csr_sign_err_count",
		Help:      "The number of erorrs occurred when signing the CSR.",
	}, []string{errorlabel})

	successCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "success_cert_issuance_count",
		Help:      "The number of certificates issuances that have succeeded.",
	}, []string{})
)

func init() {
	prometheus.MustRegister(csrCounts)
	prometheus.MustRegister(authnErrorCounts)
	prometheus.MustRegister(authzErrorCounts)
	prometheus.MustRegister(certSignErrorCounts)
	prometheus.MustRegister(successCounts)
}

// monitoringMetrics are counters for certificate signing related operations.
type monitoringMetrics struct {
	CSR            prometheus.Counter
	AuthnError     prometheus.Counter
	Success        prometheus.Counter
	authzErrors    *prometheus.CounterVec
	certSignErrors *prometheus.CounterVec
}

// newMonitoringMetrics creates a new monitoringMetrics.
func newMonitoringMetrics() monitoringMetrics {
	return monitoringMetrics{
		CSR:            csrCounts.With(prometheus.Labels{}),
		AuthnError:     authnErrorCounts.With(prometheus.Labels{}),
		Success:        successCounts.With(prometheus.Labels{}),
		authzErrors:    authzErrorCounts,
		certSignErrors: certSignErrorCounts,
	}
}

func (m *monitoringMetrics) GetAuthzError(err string) prometheus.Counter {
	return m.authzErrors.With(prometheus.Labels{errorlabel: err})
}

func (m *monitoringMetrics) GetCertSignError(err string) prometheus.Counter {
	return m.certSignErrors.With(prometheus.Labels{errorlabel: err})
}
