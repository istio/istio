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

package bootstrap

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/security/pkg/k8s/chiron"
)

const (
	// defaultCertGracePeriodRatio is the default length of certificate rotation grace period,
	// configured as the ratio of the certificate TTL.
	defaultCertGracePeriodRatio = 0.5

	// defaultMinCertGracePeriod is the default minimum grace period for workload cert rotation.
	defaultMinCertGracePeriod = 10 * time.Minute

	// Default CA certificate path
	// Currently, custom CA path is not supported; no API to get custom CA cert yet.
	defaultCACertPath = "./var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

var (
	// dnsCertDir is the location to save generated DNS certificates.
	// TODO: we can probably avoid saving, but will require deeper changes.
	dnsCertDir  = "./var/run/secrets/istio-dns"
	dnsKeyFile  = "./" + filepath.Join(dnsCertDir, "key.pem")
	dnsCertFile = "./" + filepath.Join(dnsCertDir, "cert-chain.pem")

	KubernetesCAProvider = "kubernetes"
	IstiodCAProvider     = "istiod"
)

// CertController can create certificates signed by K8S server.
func (s *Server) initCertController(args *PilotArgs) error {
	var err error
	var secretNames, dnsNames, namespaces []string

	meshConfig := s.environment.Mesh()
	if meshConfig.GetCertificates() == nil || len(meshConfig.GetCertificates()) == 0 {
		// TODO: if the provider is set to Citadel, use that instead of k8s so the API is still preserved.
		log.Info("No certificates specified, skipping K8S DNS certificate controller")
		return nil
	}

	k8sClient := s.kubeClient
	for _, c := range meshConfig.GetCertificates() {
		name := strings.Join(c.GetDnsNames(), ",")
		if len(name) == 0 { // must have a DNS name
			continue
		}
		if len(c.GetSecretName()) > 0 {
			// Chiron will generate the key and certificate and save them in a secret
			secretNames = append(secretNames, c.GetSecretName())
			dnsNames = append(dnsNames, name)
			namespaces = append(namespaces, args.Namespace)
		}
	}

	// Provision and manage the certificates for non-Pilot services.
	// If services are empty, the certificate controller will do nothing.
	s.certController, err = chiron.NewWebhookController(defaultCertGracePeriodRatio, defaultMinCertGracePeriod,
		k8sClient.CoreV1(), k8sClient.AdmissionregistrationV1beta1(), k8sClient.CertificatesV1beta1(),
		defaultCACertPath, secretNames, dnsNames, namespaces)
	if err != nil {
		return fmt.Errorf("failed to create certificate controller: %v", err)
	}
	s.addStartFunc(func(stop <-chan struct{}) error {
		go func() {
			// Run Chiron to manage the lifecycles of certificates
			s.certController.Run(stop)
		}()

		return nil
	})

	return nil
}

// initDNSCerts will create the certificates to be used by Istiod GRPC server and webhooks.
// If the certificate creation fails - for example no support in K8S - returns an error.
// Will use the mesh.yaml DiscoveryAddress to find the default expected address of the control plane,
// with an environment variable allowing override.
//
// Controlled by features.IstiodService env variable, which defines the name of the service to use in the DNS
// cert, or empty for disabling this feature.
//
// TODO: If the discovery address in mesh.yaml is set to port 15012 (XDS-with-DNS-certs) and the name
// matches the k8s namespace, failure to start DNS server is a fatal error.
func (s *Server) initDNSCerts(hostname, customHost, namespace string) error {
	// Name in the Istiod cert - support the old service names as well.
	// validate hostname contains namespace
	parts := strings.Split(hostname, ".")
	hostnamePrefix := parts[0]

	// append custom hostname if there is any
	names := []string{hostname}
	if customHost != "" && customHost != hostname {
		log.Infof("Adding custom hostname %s", customHost)
		names = append(names, customHost)
	}

	// The first is the recommended one, also used by Apiserver for webhooks.
	// add a few known hostnames
	for _, altName := range []string{"istiod", "istiod-remote", "istio-pilot"} {
		name := fmt.Sprintf("%v.%v.svc", altName, namespace)
		if name == hostname || name == customHost {
			continue
		}
		names = append(names, name)
	}

	var certChain, keyPEM []byte
	var err error
	if features.PilotCertProvider.Get() == KubernetesCAProvider {
		log.Infof("Generating K8S-signed cert for %v", names)
		certChain, keyPEM, _, err = chiron.GenKeyCertK8sCA(s.kubeClient.CertificatesV1beta1().CertificateSigningRequests(),
			strings.Join(names, ","), hostnamePrefix+".csr.secret", namespace, defaultCACertPath)

		s.caBundlePath = defaultCACertPath
	} else if features.PilotCertProvider.Get() == IstiodCAProvider {
		log.Infof("Generating istiod-signed cert for %v", names)
		certChain, keyPEM, err = s.CA.GenKeyCert(names, SelfSignedCACertTTL.Get())

		signingKeyFile := path.Join(LocalCertDir.Get(), "ca-key.pem")
		// check if signing key file exists the cert dir
		if _, err := os.Stat(signingKeyFile); err != nil {
			log.Infof("No plugged-in cert at %v; self-signed cert is used", signingKeyFile)

			// When Citadel is configured to use self-signed certs, keep a local copy so other
			// components can load it via file (e.g. webhook config controller).
			if err := os.MkdirAll(dnsCertDir, 0700); err != nil {
				return err
			}
			// We have direct access to the self-signed
			internalSelfSignedRootPath := path.Join(dnsCertDir, "self-signed-root.pem")

			rootCert := s.CA.GetCAKeyCertBundle().GetRootCertPem()
			if err = ioutil.WriteFile(internalSelfSignedRootPath, rootCert, 0600); err != nil {
				return err
			}

			s.addStartFunc(func(stop <-chan struct{}) error {
				go func() {
					for {
						select {
						case <-stop:
							return
						case <-time.After(controller.NamespaceResyncPeriod):
							newRootCert := s.CA.GetCAKeyCertBundle().GetRootCertPem()
							if !bytes.Equal(rootCert, newRootCert) {
								rootCert = newRootCert
								if err = ioutil.WriteFile(internalSelfSignedRootPath, rootCert, 0600); err != nil {
									log.Errorf("Failed to update local copy of self-signed root: %v", err)
								} else {
									log.Info("Updated local copy of self-signed root")
								}
							}
						}
					}
				}()
				return nil
			})
			s.caBundlePath = internalSelfSignedRootPath
		} else {
			log.Infof("Use plugged-in cert at %v", signingKeyFile)
			s.caBundlePath = path.Join(LocalCertDir.Get(), "root-cert.pem")
		}

	} else {
		log.Infof("User specified cert provider: %v", features.PilotCertProvider.Get())
		return nil
	}
	if err != nil {
		return err
	}

	// Save the certificates to ./var/run/secrets/istio-dns - this is needed since most of the code we currently
	// use to start grpc and webhooks is based on files. This is a memory-mounted dir.
	if err := os.MkdirAll(dnsCertDir, 0700); err != nil {
		return err
	}
	err = ioutil.WriteFile(dnsKeyFile, keyPEM, 0600)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(dnsCertFile, certChain, 0600)
	if err != nil {
		return err
	}
	log.Infoa("DNS certificates created in ", dnsCertDir)
	return nil
}
