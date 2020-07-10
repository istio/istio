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

package istioagentutil

import (
	"io/ioutil"
	"path"
	"strings"

	"istio.io/istio/pkg/config/constants"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	"istio.io/pkg/log"
)

var (
	// CitadelCACertPath is the directory for Citadel CA certificate.
	// This is mounted from config map 'istio-ca-root-cert'. Part of startup,
	// this may be replaced with ./etc/certs, if a root-cert.pem is found, to
	// handle secrets mounted from non-citadel CAs.
	CitadelCACertPath = "./var/run/secrets/istio"

	// Location of K8S CA root.
	k8sCAPath = "./var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// DefaultRootCertFilePath is the well-known path for an existing root certificate file
	CacheDefaultRootCertFilePath = "./etc/certs/root-cert.pem"
)

func NewIstiodCAClient(pilotCertProvider, caEndpoint, clusterId string, TLSEnabled, isRotate bool) (caClientInterface.Client, error, []byte) {
	// TODO: this should all be packaged in a plugin, possibly with optional compilation.
	log.Infof("caEndpoint == %v", caEndpoint)
	var err error
	var caClient caClientInterface.Client
	var rootCert []byte
		// Determine the default CA.
		// If /etc/certs exists - it means Citadel is used (possibly in a mode to only provision the root-cert, not keys)
		// Otherwise: default to istiod
		//
		// If an explicit CA is configured, assume it is mounting /etc/certs

		tls := true
		certReadErr := false

		if caEndpoint == "" {
			// When caEndpoint is nil, the default CA endpoint
			// will be a hardcoded default value (e.g., the namespace will be hardcoded
			// as istio-system).
			log.Info("Istio Agent uses default istiod CA")
			caEndpoint = "istiod.istio-system.svc:15012"

			if pilotCertProvider == "istiod" {
				log.Info("istiod uses self-issued certificate")
				if rootCert, err = ioutil.ReadFile(path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)); err != nil {
					certReadErr = true
				} else {
					log.Infof("the CA cert of istiod is: %v", string(rootCert))
				}
			} else if pilotCertProvider == "kubernetes" {
				log.Infof("istiod uses the k8s root certificate %v", k8sCAPath)
				if rootCert, err = ioutil.ReadFile(k8sCAPath); err != nil {
					certReadErr = true
				}
			} else if pilotCertProvider == "custom" {
				log.Infof("istiod uses a custom root certificate mounted in a well known location %v",
					CacheDefaultRootCertFilePath)
				if rootCert, err = ioutil.ReadFile(CacheDefaultRootCertFilePath); err != nil {
					certReadErr = true
				}
			} else {
				certReadErr = true
			}
			if certReadErr {
				rootCert = nil
				// for debugging only
				log.Warnf("Failed to load root cert, assume IP secure network: %v", err)
				caEndpoint = "istiod.istio-system.svc:15010"
				tls = false
			}
		} else {
			// Explicitly configured CA
			log.Infoa("Using user-configured CA ", caEndpoint)
			if strings.HasSuffix(caEndpoint, ":15010") {
				log.Warna("Debug mode or IP-secure network")
				tls = false
			} else if TLSEnabled {
				if pilotCertProvider == "istiod" {
					log.Info("istiod uses self-issued certificate")
					if rootCert, err = ioutil.ReadFile(path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)); err != nil {
						certReadErr = true
					} else {
						log.Infof("the CA cert of istiod is: %v", string(rootCert))
					}
				} else if pilotCertProvider == "kubernetes" {
					log.Infof("istiod uses the k8s root certificate %v", k8sCAPath)
					if rootCert, err = ioutil.ReadFile(k8sCAPath); err != nil {
						certReadErr = true
					}
				} else if pilotCertProvider == "custom" {
					log.Infof("istiod uses a custom root certificate mounted in a well known location %v",
						CacheDefaultRootCertFilePath)
					if rootCert, err = ioutil.ReadFile(CacheDefaultRootCertFilePath); err != nil {
						certReadErr = true
					}
				} else {
					log.Errorf("unknown cert provider %v", pilotCertProvider)
					certReadErr = true
				}
				if certReadErr {
					rootCert = nil
					log.Fatal("invalid config - port 15012 missing a root certificate")
				}
			} else {
				rootCertPath := path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)
				if rootCert, err = ioutil.ReadFile(rootCertPath); err != nil {
					// We may not provide root cert, and can just use public system certificate pool
					log.Infof("no certs found at %v, using system certs", rootCertPath)
				} else {
					log.Infof("the CA cert of istiod is: %v", string(rootCert))
				}
			}
		}

		// Will use TLS unless the reserved 15010 port is used ( istiod on an ipsec/secure VPC)
		// rootCert may be nil - in which case the system roots are used, and the CA is expected to have public key
		// Otherwise assume the injection has mounted /etc/certs/root-cert.pem
		caClient, err = citadel.NewCitadelClient(caEndpoint, tls, rootCert, clusterId ,isRotate)

	return caClient, err, rootCert
}