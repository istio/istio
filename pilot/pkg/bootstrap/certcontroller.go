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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/k8s/chiron"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/pkg/log"
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
		k8sClient.CoreV1(), k8sClient.CertificatesV1beta1(),
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

	var certChain, keyPEM, caBundle []byte
	var err error
	if features.PilotCertProvider == constants.CertProviderKubernetes {
		log.Infof("Generating K8S-signed cert for %v", names)
		certChain, keyPEM, _, err = chiron.GenKeyCertK8sCA(s.kubeClient.CertificatesV1beta1().CertificateSigningRequests(),
			strings.Join(names, ","), hostnamePrefix+".csr.secret", namespace, defaultCACertPath, "")
		if err != nil {
			return fmt.Errorf("failed generating key and cert by kubernetes: %v", err)
		}
		caBundle, err = ioutil.ReadFile(defaultCACertPath)
		if err != nil {
			return fmt.Errorf("failed reading %s: %v", defaultCACertPath, err)
		}
	} else if features.PilotCertProvider == constants.CertProviderIstiod {
		certChain, keyPEM, err = s.CA.GenKeyCert(names, SelfSignedCACertTTL.Get(), false)
		if err != nil {
			return fmt.Errorf("failed generating istiod key cert %v", err)
		}
		log.Infof("Generating istiod-signed cert for %v:\n %s", names, certChain)

		signingKeyFile := path.Join(LocalCertDir.Get(), ca.CAPrivateKeyFile)
		// check if signing key file exists the cert dir
		if _, err := os.Stat(signingKeyFile); err != nil {
			log.Infof("No plugged-in cert at %v; self-signed cert is used", signingKeyFile)
			caBundle = s.CA.GetCAKeyCertBundle().GetRootCertPem()
			s.addStartFunc(func(stop <-chan struct{}) error {
				go func() {
					// regenerate istiod key cert when root cert changes.
					s.watchRootCertAndGenKeyCert(names, stop)
				}()
				return nil
			})
		} else {
			log.Infof("Use plugged-in cert at %v", signingKeyFile)
			caBundle, err = ioutil.ReadFile(path.Join(LocalCertDir.Get(), ca.RootCertFile))
			if err != nil {
				return fmt.Errorf("failed reading %s: %v", path.Join(LocalCertDir.Get(), ca.RootCertFile), err)
			}
		}
	} else {
		customCACertPath := security.DefaultRootCertFilePath
		log.Infof("User specified cert provider: %v, mounted in a well known location %v",
			features.PilotCertProvider, customCACertPath)
		caBundle, err = ioutil.ReadFile(customCACertPath)
		if err != nil {
			return fmt.Errorf("failed reading %s: %v", customCACertPath, err)
		}
	}
	s.istiodCertBundleWatcher.SetAndNotify(keyPEM, certChain, caBundle)
	return nil
}

// TODO(hzxuzonghu): support async notification instead of polling the CA root cert.
func (s *Server) watchRootCertAndGenKeyCert(names []string, stop <-chan struct{}) {
	caBundle := s.CA.GetCAKeyCertBundle().GetRootCertPem()
	for {
		select {
		case <-stop:
			return
		case <-time.After(controller.NamespaceResyncPeriod):
			newRootCert := s.CA.GetCAKeyCertBundle().GetRootCertPem()
			if !bytes.Equal(caBundle, newRootCert) {
				caBundle = newRootCert
				certChain, keyPEM, err := s.CA.GenKeyCert(names, SelfSignedCACertTTL.Get(), false)
				if err != nil {
					log.Errorf("failed generating istiod key cert %v", err)
				} else {
					s.istiodCertBundleWatcher.SetAndNotify(keyPEM, certChain, caBundle)
					log.Infof("regenerated istiod dns cert: %s", certChain)
				}
			}
		}
	}
}

// initCertificateWatches sets up watches for the plugin dns certs.
func (s *Server) initCertificateWatches(tlsOptions TLSOptions) error {
	if err := s.istiodCertBundleWatcher.SetFromFilesAndNotify(tlsOptions.KeyFile, tlsOptions.CertFile, tlsOptions.CaCertFile); err != nil {
		return fmt.Errorf("set keyCertBundle failed: %v", err)
	}
	// TODO: Setup watcher for root and restart server if it changes.
	for _, file := range []string{tlsOptions.CertFile, tlsOptions.KeyFile} {
		log.Infof("adding watcher for certificate %s", file)
		if err := s.fileWatcher.Add(file); err != nil {
			return fmt.Errorf("could not watch %v: %v", file, err)
		}
	}
	s.addStartFunc(func(stop <-chan struct{}) error {
		go func() {
			var keyCertTimerC <-chan time.Time
			for {
				select {
				case <-keyCertTimerC:
					keyCertTimerC = nil
					if err := s.istiodCertBundleWatcher.SetFromFilesAndNotify(tlsOptions.KeyFile, tlsOptions.CertFile, tlsOptions.CaCertFile); err != nil {
						log.Errorf("Setting keyCertBundle failed: %v", err)
					}
				case <-s.fileWatcher.Events(tlsOptions.CertFile):
					if keyCertTimerC == nil {
						keyCertTimerC = time.After(watchDebounceDelay)
					}
				case <-s.fileWatcher.Events(tlsOptions.KeyFile):
					if keyCertTimerC == nil {
						keyCertTimerC = time.After(watchDebounceDelay)
					}
				case err := <-s.fileWatcher.Errors(tlsOptions.CertFile):
					log.Errorf("error watching %v: %v", tlsOptions.CertFile, err)
				case err := <-s.fileWatcher.Errors(tlsOptions.KeyFile):
					log.Errorf("error watching %v: %v", tlsOptions.KeyFile, err)
				case <-stop:
					return
				}
			}
		}()
		return nil
	})
	return nil
}

func (s *Server) reloadIstiodCert(watchCh <-chan keycertbundle.KeyCertBundle, stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		default:
			if err := s.loadIstiodCert(watchCh, stopCh); err != nil {
				log.Errorf("reload istiod cert failed: %v", err)
			}
		}
	}
}

// loadIstiodCert load IstiodCert received from watchCh once
func (s *Server) loadIstiodCert(watchCh <-chan keycertbundle.KeyCertBundle, stopCh <-chan struct{}) error {
	var keyCertBundle keycertbundle.KeyCertBundle
	select {
	case keyCertBundle = <-watchCh:
	case <-stopCh:
		return nil
	}
	keyPair, err := tls.X509KeyPair(keyCertBundle.CertPem, keyCertBundle.KeyPem)
	if err != nil {
		return fmt.Errorf("istiod loading x509 key pairs failed: %v", err)
	}
	for _, c := range keyPair.Certificate {
		x509Cert, err := x509.ParseCertificates(c)
		if err != nil {
			// This can rarely happen, just in case.
			return fmt.Errorf("x509 cert - ParseCertificates() error: %v", err)
		}
		for _, c := range x509Cert {
			log.Infof("x509 cert - Issuer: %q, Subject: %q, SN: %x, NotBefore: %q, NotAfter: %q",
				c.Issuer, c.Subject, c.SerialNumber,
				c.NotBefore.Format(time.RFC3339), c.NotAfter.Format(time.RFC3339))
		}
	}

	log.Info("Istiod certificates are reloaded")
	s.certMu.Lock()
	s.istiodCert = &keyPair
	s.certMu.Unlock()
	return nil
}
