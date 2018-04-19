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
	configID = "CitadelServer"
)

var (
	citadelLabels = []string{configID}

	csrCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "csr_count",
		Help:      "The number of CSRs recerived by Citadel server.",
	}, citadelLabels)

	authenticationErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "authentication_failure_count",
		Help:      "The number of authentication failures.",
	}, citadelLabels)

	csrParsingErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "csr_parsing_err_count",
		Help:      "The number of erorrs occurred when parsing the CSR.",
	}, citadelLabels)

	idExtractionErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "id_extraction_err_count",
		Help:      "The number of erorrs occurred when extracting the ID from CSR.",
	}, citadelLabels)

	csrSignErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "csr_sign_err_count",
		Help:      "The number of erorrs occurred when signing the CSR.",
	}, citadelLabels)

	successCertIssuanceCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "server",
		Name:      "success_cert_issuance_count",
		Help:      "The number of certificates issuances that have succeeded.",
	}, citadelLabels)
)

func init() {
	prometheus.MustRegister(csrCount)
	prometheus.MustRegister(authenticationErrorCount)
	prometheus.MustRegister(csrParsingErrorCount)
	prometheus.MustRegister(idExtractionErrorCount)
	prometheus.MustRegister(csrSignErrorCount)
	prometheus.MustRegister(successCertIssuanceCount)
}

// monitoringMetrics are counters for certificate signing related operations.
type monitoringMetrics struct {
	SuccessCertIssuance prometheus.Counter
	CSR                 prometheus.Counter
	AuthenticationError prometheus.Counter
	CSRParsingError     prometheus.Counter
	IDExtractionError   prometheus.Counter
	CSRSignError        prometheus.Counter
}

// newMonitoringMetrics creates a new monitoringMetrics.
func newMonitoringMetrics() monitoringMetrics {
	labels := prometheus.Labels{
		configID: "1",
	}
	return monitoringMetrics{
		SuccessCertIssuance: successCertIssuanceCount.With(labels),
		CSR:                 csrCount.With(labels),
		AuthenticationError: authenticationErrorCount.With(labels),
		CSRParsingError:     csrParsingErrorCount.With(labels),
		IDExtractionError:   idExtractionErrorCount.With(labels),
		CSRSignError:        csrSignErrorCount.With(labels),
	}
}
