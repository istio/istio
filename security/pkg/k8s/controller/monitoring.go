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
	"istio.io/pkg/monitoring"
)

const (
	errorlabel = "error"
)

var (
	errorTag = monitoring.MustCreateLabel(errorlabel)

	serviceAccountCreationCounts = monitoring.NewSum(
		"citadel_secret_controller_svc_acc_created_cert_count",
		"The number of certificates created due to service account creation.",
	)

	serviceAccountDeletionCounts = monitoring.NewSum(
		"citadel_secret_controller_svc_acc_deleted_cert_count",
		"The number of certificates deleted due to service account deletion.",
	)

	secretDeletionCounts = monitoring.NewSum(
		"citadel_secret_controller_secret_deleted_cert_count",
		"The number of certificates recreated due to secret deletion (service account still exists).",
	)

	csrErrorCounts = monitoring.NewSum(
		"citadel_secret_controller_csr_err_count",
		"The number of errors occurred when creating the CSR.",
	)

	certSignErrorCounts = monitoring.NewSum(
		"citadel_secret_controller_csr_sign_err_count",
		"The number of errors occurred when signing the CSR.",
		monitoring.WithLabels(errorTag),
	)
)

func init() {
	monitoring.MustRegister(
		serviceAccountCreationCounts,
		serviceAccountDeletionCounts,
		secretDeletionCounts,
		csrErrorCounts,
		certSignErrorCounts,
	)
}

// monitoringMetrics are counters for secret controller operations.
type monitoringMetrics struct {
	ServiceAccountCreation monitoring.Metric
	ServiceAccountDeletion monitoring.Metric
	SecretDeletion         monitoring.Metric
	CSRError               monitoring.Metric
	certSignErrors         monitoring.Metric
}

// newMonitoringMetrics creates a new monitoringMetrics.
func newMonitoringMetrics() monitoringMetrics {
	return monitoringMetrics{
		ServiceAccountCreation: serviceAccountCreationCounts,
		ServiceAccountDeletion: serviceAccountDeletionCounts,
		SecretDeletion:         secretDeletionCounts,
		CSRError:               csrErrorCounts,
		certSignErrors:         certSignErrorCounts,
	}
}

func (m *monitoringMetrics) GetCertSignError(err string) monitoring.Metric {
	return m.certSignErrors.With(errorTag.Value(err))
}
