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

package cmd

import "time"

const (
	// DefaultSelfSignedCACertTTL is the default TTL of self-signed CA root certificate.
	DefaultSelfSignedCACertTTL = 3650 * 24 * time.Hour

	// DefaultSelfSignedRootCertCheckInterval is the default interval a self-signed
	// CA checks and rotates its root certificate.
	DefaultSelfSignedRootCertCheckInterval = 1 * time.Hour

	// DefaultRootCertGracePeriodPercentile is the default length of root certificate
	// rotation grace period, configured as the ratio of the certificate TTL.
	DefaultRootCertGracePeriodPercentile = 20

	// ReadSigningCertRetryInterval specifies the time to wait between retries on reading the signing key and cert.
	ReadSigningCertRetryInterval = time.Second * 5

	// ReadSigningCertRetryMax specifies the total max time to wait between retries on reading the signing key and cert.
	ReadSigningCertRetryMax = time.Second * 30

	// DefaultMaxWorkloadCertTTL is the default max TTL of issued workload certificates.
	DefaultMaxWorkloadCertTTL = 90 * 24 * time.Hour

	// DefaultWorkloadCertTTL is the default TTL of issued workload certificates.
	DefaultWorkloadCertTTL = 24 * time.Hour
)
