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

package bootstrap

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"istio.io/istio/security/pkg/k8s/chiron"
	"istio.io/pkg/log"
)

const (
	// defaultCertGracePeriodRatio is the default length of certificate rotation grace period,
	// configured as the ratio of the certificate TTL.
	defaultCertGracePeriodRatio = 0.5

	// defaultMinCertGracePeriod is the default minimum grace period for workload cert rotation.
	defaultMinCertGracePeriod = 10 * time.Minute

	// Default directory to store Pilot key and certificate under $HOME directory
	defaultDirectoryForKeyCert = "/pilot/key-cert"

	// Default CA certificate path
	// Currently, custom CA path is not supported; no API to get custom CA cert yet.
	defaultCACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

func (s *Server) initCertController(args *PilotArgs) error {
	var err error
	var secretNames, dnsNames, namespaces []string
	// Whether a key and cert are generated for Pilot
	var pilotCertGenerated bool

	if s.Mesh.GetCertificates() == nil || len(s.Mesh.GetCertificates()) == 0 {
		log.Info("nil certificate config")
		return nil
	}

	k8sClient := s.kubeClient
	for _, c := range s.Mesh.GetCertificates() {
		name := strings.Join(c.GetDnsNames(), ",")
		if len(name) == 0 { // must have a DNS name
			continue
		}
		if len(c.GetSecretName()) > 0 { //
			// Chiron will generate the key and certificate and save them in a secret
			secretNames = append(secretNames, c.GetSecretName())
			dnsNames = append(dnsNames, name)
			namespaces = append(namespaces, args.Namespace)
		} else if !pilotCertGenerated {
			// Generate a key and certificate for the service and save them into a hard-coded directory.
			// Only one service (currently Pilot) will save the key and certificate in a directory.
			// Create directory at s.mesh.K8SCertificateSetting.PilotCertificatePath if it doesn't exist.
			svcName := "istio.pilot"
			dir, err := pilotDnsCertDir()
			if err != nil {
				return err
			}
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				err := os.MkdirAll(dir, os.ModePerm)
				if err != nil {
					return fmt.Errorf("err to create directory %v: %v", dir, err)
				}
			}
			// Generate Pilot certificate
			certChain, keyPEM, caCert, err := chiron.GenKeyCertK8sCA(k8sClient.CertificatesV1beta1().CertificateSigningRequests(),
				name, svcName+".csr.secret", args.Namespace, defaultCACertPath)
			if err != nil {
				log.Errorf("err to generate key cert for %v: %v", svcName, err)
				return nil
			}
			// Save cert-chain.pem, root.pem, and key.pem to the directory.
			file := path.Join(dir, "cert-chain.pem")
			if err = ioutil.WriteFile(file, certChain, 0644); err != nil {
				log.Errorf("err to write %v cert-chain.pem (%v): %v", svcName, file, err)
				return nil
			}
			file = path.Join(dir, "root.pem")
			if err = ioutil.WriteFile(file, caCert, 0644); err != nil {
				log.Errorf("err to write %v root.pem (%v): %v", svcName, file, err)
				return nil
			}
			file = path.Join(dir, "key.pem")
			if err = ioutil.WriteFile(file, keyPEM, 0600); err != nil {
				log.Errorf("err to write %v key.pem (%v): %v", svcName, file, err)
				return nil
			}
			pilotCertGenerated = true
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

func pilotDnsCertDir() (string, error) {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find local user folder: %v", err)
	}
	return userHomeDir + defaultDirectoryForKeyCert, nil
}
