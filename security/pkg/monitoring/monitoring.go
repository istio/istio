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

package monitoring

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	configID = "configID"
)

var (
	caLabels = []string{configID}

	successCertIssuanceCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "ca",
		Name:      "success_cert_issuance_count",
		Help:      "The number of certificates issuances that have succeeded.",
	}, caLabels)

	cSRErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "ca",
		Name:      "csr_err_count",
		Help:      "The number of CSR errors seen by Citadel.",
	}, caLabels)

	tTLErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "ca",
		Name:      "ttl_err_count",
		Help:      "The number of erorrs occurred due to requested TTL is too large.",
	}, caLabels)

	certGenerationErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "ca",
		Name:      "cert_gen_err_count",
		Help:      "The number of erorrs occurred when generating certificate from validated CSR.",
	}, caLabels)

	serviceAccountCreationCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "certs_created_due_to_service_account_creation",
		Help:      "The number of certificates created due to service account creation.",
	}, caLabels)

	serviceAccountDeletionCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "certs_deleted_due_to_service_account_deletion",
		Help:      "The number of certificates deleted due to service account deletion.",
	}, caLabels)

	secretDeletionCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "citadel",
		Subsystem: "secret_controller",
		Name:      "certs_created_due_to_secret_deletion",
		Help:      "The number of certificates recreated due to secret deletion (service account still exists).",
	}, caLabels)
)

func init() {
	prometheus.MustRegister(successCertIssuanceCount)
	prometheus.MustRegister(cSRErrorCount)
	prometheus.MustRegister(tTLErrorCount)
	prometheus.MustRegister(certGenerationErrorCount)
	prometheus.MustRegister(serviceAccountCreationCount)
	prometheus.MustRegister(serviceAccountDeletionCount)
	prometheus.MustRegister(secretDeletionCount)
}

// CAOperationCounters are counters for certificate signing related operations.
type CAOperationCounters struct {
	SuccessCertIssuance prometheus.Counter
	CSRError            prometheus.Counter
	TTLError            prometheus.Counter
	CertGenerationError prometheus.Counter
}

// NewCAOperationCounters creates a new CAOperationCounters.
func NewCAOperationCounters(cfgID int64) CAOperationCounters {
	labels := prometheus.Labels{
		configID: strconv.FormatInt(cfgID, 10),
	}
	return CAOperationCounters{
		SuccessCertIssuance: successCertIssuanceCount.With(labels),
		CSRError:            cSRErrorCount.With(labels),
		TTLError:            tTLErrorCount.With(labels),
		CertGenerationError: certGenerationErrorCount.With(labels),
	}
}

// SecretControllerOperationCounters are counters for secret controller operations.
type SecretControllerOperationCounters struct {
	ServiceAccountCreation prometheus.Counter
	ServiceAccountDeletion prometheus.Counter
	SecretDeletion         prometheus.Counter
}

// NewSecretControllerOperationCounters creates a new SecretControllerOperationCounters.
func NewSecretControllerOperationCounters(cfgID int64) SecretControllerOperationCounters {
	labels := prometheus.Labels{
		configID: strconv.FormatInt(cfgID, 10),
	}
	return SecretControllerOperationCounters{
		ServiceAccountCreation: serviceAccountCreationCount.With(labels),
		ServiceAccountDeletion: serviceAccountDeletionCount.With(labels),
		SecretDeletion:         secretDeletionCount.With(labels),
	}
}
