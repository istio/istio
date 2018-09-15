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

package cmd

import "time"

const (
	// DefaultSelfSignedCACertTTL is the default TTL of self-signed CA root certificate.
	DefaultSelfSignedCACertTTL = 365 * 24 * time.Hour

	// DefaultRequestedCACertTTL is the default requested TTL for the workload.
	DefaultRequestedCACertTTL = 365 * 24 * time.Hour

	// DefaultMaxWorkloadCertTTL is the default max TTL of issued workload certificates.
	DefaultMaxWorkloadCertTTL = 90 * 24 * time.Hour

	// DefaultWorkloadCertTTL is the default TTL of issued workload certificates.
	DefaultWorkloadCertTTL = 90 * 24 * time.Hour

	// DefaultWorkloadCertGracePeriodRatio is the default length of certificate rotation grace period,
	// configured as the ratio of the certificate TTL.
	DefaultWorkloadCertGracePeriodRatio = 0.5

	// DefaultWorkloadMinCertGracePeriod is the default minimum grace period for workload cert rotation.
	DefaultWorkloadMinCertGracePeriod = 10 * time.Minute

	// DefaultProbeCheckInterval is the default interval of checking the liveness of the CA.
	DefaultProbeCheckInterval = 30 * time.Second

	// DefaultCSRGracePeriodPercentage is the default length of certificate rotation grace period,
	// configured as the percentage of the certificate TTL.
	DefaultCSRGracePeriodPercentage = 50

	// DefaultCSRInitialRetrialInterval is the default initial interval between retries to send CSR to upstream CA.
	DefaultCSRInitialRetrialInterval = time.Second

	// DefaultCSRMaxRetries is the default value of CSR retries for Citadel to send CSR to upstream CA.
	DefaultCSRMaxRetries = 10

	// ListenedNamespaceKey is the key for the environment variable that specifies the namespace.
	ListenedNamespaceKey = "NAMESPACE"

	// IstioCaClusterDomainKey is the key for the environment variable that specifies the cluster domain.
	IstioCaClusterDomainKey = "DNS_DOMAIN"
)
