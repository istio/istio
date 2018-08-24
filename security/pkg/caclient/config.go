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

package caclient

import (
	"time"
)

// Config is configuration for the CA client.
type Config struct {
	// Address of the CA which the CA client calls to
	CAAddress string

	// Organization presented in the certificates
	Org string

	// Requested TTL of the certificates
	RequestedCertTTL time.Duration

	// Size of RSA private key
	RSAKeySize int

	// The environment this CA client is running on.
	Env string

	// The cluster management platform this ndoe agent is running on.
	Platform string

	// Whether the certificate is for CA
	ForCA bool

	// CSRInitialRetrialInterval is the retrial interval for certificate requests.
	CSRInitialRetrialInterval time.Duration

	// CSRMaxRetries is the number of retries for certificate requests.
	CSRMaxRetries int

	// CSRGracePeriodPercentage indicates the length of the grace period in the
	// percentage of the entire certificate TTL.
	CSRGracePeriodPercentage int

	// CertFile defines the cert of the CA client.
	CertFile string

	// CertChainFile defines the cert chain file of the CA client, including the client's cert.
	CertChainFile string

	// KeyFile defines the private key of the CA client.
	KeyFile string

	// RootCertFile defines the root cert of the CA client.
	RootCertFile string

	// K8sServiceAccountFile defines the k8s service account used for CSR.
	K8sServiceAccountFile string
}
