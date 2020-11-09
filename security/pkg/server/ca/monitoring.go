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

package ca

import (
	"istio.io/pkg/monitoring"
)

const (
	errorlabel = "error"
)

var (
	errorTag = monitoring.MustCreateLabel(errorlabel)

	csrCounts = monitoring.NewSum(
		"citadel_server_csr_count",
		"The number of CSRs received by Citadel server.",
	)

	authnErrorCounts = monitoring.NewSum(
		"citadel_server_authentication_failure_count",
		"The number of authentication failures.",
	)

	csrParsingErrorCounts = monitoring.NewSum(
		"citadel_server_csr_parsing_err_count",
		"The number of errors occurred when parsing the CSR.",
	)

	idExtractionErrorCounts = monitoring.NewSum(
		"citadel_server_id_extraction_err_count",
		"The number of errors occurred when extracting the ID from CSR.",
	)

	certSignErrorCounts = monitoring.NewSum(
		"citadel_server_csr_sign_err_count",
		"The number of errors occurred when signing the CSR.",
		monitoring.WithLabels(errorTag),
	)

	successCounts = monitoring.NewSum(
		"citadel_server_success_cert_issuance_count",
		"The number of certificates issuances that have succeeded.",
	)

	rootCertExpiryTimestamp = monitoring.NewGauge(
		"citadel_server_root_cert_expiry_timestamp",
		"The unix timestamp, in seconds, when Citadel root cert will expire. "+
			"A negative time indicates the cert is expired.",
	)
	certChainExpiryTimestamp = monitoring.NewGauge(
		"citadel_server_cert_chain_expiry_timestamp",
		"The unix timestamp, in seconds, when Citadel cert chain will expire. "+
			"A negative time indicates the cert is expired.",
	)
)

func init() {
	monitoring.MustRegister(
		csrCounts,
		authnErrorCounts,
		csrParsingErrorCounts,
		idExtractionErrorCounts,
		certSignErrorCounts,
		successCounts,
		rootCertExpiryTimestamp,
		certChainExpiryTimestamp,
	)
}

// monitoringMetrics are counters for certificate signing related operations.
type monitoringMetrics struct {
	CSR               monitoring.Metric
	AuthnError        monitoring.Metric
	Success           monitoring.Metric
	CSRError          monitoring.Metric
	IDExtractionError monitoring.Metric
	certSignErrors    monitoring.Metric
}

// newMonitoringMetrics creates a new monitoringMetrics.
func newMonitoringMetrics() monitoringMetrics {
	return monitoringMetrics{
		CSR:               csrCounts,
		AuthnError:        authnErrorCounts,
		Success:           successCounts,
		CSRError:          csrParsingErrorCounts,
		IDExtractionError: idExtractionErrorCounts,
		certSignErrors:    certSignErrorCounts,
	}
}

func (m *monitoringMetrics) GetCertSignError(err string) monitoring.Metric {
	return m.certSignErrors.With(errorTag.Value(err))
}
