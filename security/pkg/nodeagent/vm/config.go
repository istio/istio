// Copyright 2019 Istio Authors
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

package vm

import (
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/caclient"
)

// Set of environment variables for VM Citadel Agent
const (
	// Name of the org for the cert.
	OrgName = "ISTIO_CA_ORG_NAME"
	// The requested TTL for the workload
	RequestedCertTTL = "ISTIO_CERT_TTL"
	// Size of the generated private key
	RSAKeySize = "ISTIO_RSA_KEY_SIZE"
	// Name of the CA, e.g. Citadel.
	CAProvider = "ISTIO_CA_PROVIDER"
	// CA endpoint.
	CAAddress = "ISTIO_CA_ADDR"
	// Whether this is a new or old protocol
	CAProtocol = "ISTIO_CA_PROTOCOL"
	// The environment in which the node agent run, e.g. GCP, on premise, etc.
	NodeEnv = "ISTIO_NODE_ENV"
	// The platform in which the node agent run, e.g. vm or k8s.
	NodePlatform = "ISTIO_NODE_PLATFORM"
	// Citadel Agent identity cert file
	CertChainFile = "ISTIO_CERT_CHAIN_FILE"
	// Citadel Agent private key file
	KeyFile = "ISTIO_KEY_FILE"
	// Citadel Agent root cert file
	RootCertFile = "ISTIo_ROOT_CERT_FILE"
	// Enable dual-use mode. Generates certificates with a CommonName identical to the SAN
	DualUse = "ISTIO_DUAL_USE"
)

const (
	VMPlatform  = "vm"
	K8sPlatform = "k8s"

	// Istio CA gRPC services.
	IstioCAService          = "IstioCAService"
	IstioCertificateService = "IstioCertificateService"
)

const (
	// defaultCSRInitialRetrialInterval is the default value of Config.CSRInitialRetrialInterval.
	defaultCSRInitialRetrialInterval = time.Second * 5
	// defaultCSRMaxRetries is the default value of Config.CSRMaxRetries.
	defaultCSRMaxRetries = 5
	// defaultCSRGracePeriodPercentage is the default value of Config.CSRGracePeriodPercentage.
	defaultCSRGracePeriodPercentage = 50
)

// Config is Node agent configuration.
type Config struct {
	// Config for CA Client
	CAClientConfig caclient.Config

	// LoggingOptions is the options for Istio logging.
	LoggingOptions *log.Options

	// SecretDirectory is the directory to to save keys/certs when using file mode SecretServer.
	SecretDirectory string

	// DualUse defines whether the generated CSRs are for dual-use mode (SAN+CN).
	DualUse bool
}

// NewConfig creates a new Config instance with default values.
func NewConfig() *Config {
	return &Config{
		CAClientConfig: caclient.Config{
			CSRInitialRetrialInterval: defaultCSRInitialRetrialInterval,
			CSRMaxRetries:             defaultCSRMaxRetries,
			CSRGracePeriodPercentage:  defaultCSRGracePeriodPercentage,
		},
		LoggingOptions: log.DefaultOptions(),
	}
}
