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

package istioagent

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/security"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
)

// WARNING WARNING WARNING
// These interfaces must be stable to allow vendors plug custom CAs.

type RootCertProvider interface {
	GetKeyCertsForCA() (string, string)
	FindRootCAForCA() (string, error)
}

var providers = make(map[string]func(*security.Options, RootCertProvider) (security.Client, error))

func createCitadel(opts *security.Options, a RootCertProvider) (security.Client, error) {
	// Using citadel CA
	var tlsOpts *citadel.TLSOptions
	var err error
	// Special case: if Istiod runs on a secure network, on the default port, don't use TLS
	// TODO: may add extra cases or explicit settings - but this is a rare use cases, mostly debugging
	if strings.HasSuffix(opts.CAEndpoint, ":15010") {
		log.Warn("Debug mode or IP-secure network")
	} else {
		tlsOpts = &citadel.TLSOptions{}
		tlsOpts.RootCert, err = a.FindRootCAForCA()
		if err != nil {
			return nil, fmt.Errorf("failed to find root CA cert for CA: %v", err)
		}

		if tlsOpts.RootCert == "" {
			log.Infof("Using CA %s cert with system certs", opts.CAEndpoint)
		} else if !fileExists(tlsOpts.RootCert) {
			log.Fatalf("invalid config - %s missing a root certificate %s", opts.CAEndpoint, tlsOpts.RootCert)
		} else {
			log.Infof("Using CA %s cert with certs: %s", opts.CAEndpoint, tlsOpts.RootCert)
		}

		tlsOpts.Key, tlsOpts.Cert = a.GetKeyCertsForCA()
	}

	// Will use TLS unless the reserved 15010 port is used ( istiod on an ipsec/secure VPC)
	// rootCert may be nil - in which case the system roots are used, and the CA is expected to have public key
	// Otherwise assume the injection has mounted /etc/certs/root-cert.pem
	return citadel.NewCitadelClient(opts, tlsOpts)
}

func init() {
	providers["Citadel"] = createCitadel
}

func createCAClient(opts *security.Options, a RootCertProvider) (security.Client, error) {
	provider, ok := providers[opts.CAProviderName]
	if !ok {
		return nil, fmt.Errorf("CA provider %q not registered", opts.CAProviderName)
	}
	return provider(opts, a)
}
