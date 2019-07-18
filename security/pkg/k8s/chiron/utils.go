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

package chiron

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"time"

	"istio.io/istio/pkg/spiffe"

	istioutil "istio.io/istio/pkg/util"

	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"

	cert "k8s.io/api/certificates/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Generate a certificate and key from k8s CA
// Working flow:
// 1. Generate a CSR
// 2. Submit a CSR
// 3. Approve a CSR
// 4. Read the signed certificate
// 5. Clean up the artifacts (e.g., delete CSR)
func GenKeyCertK8sCA(wc *WebhookController, secretName string, secretNamespace, svcName string) ([]byte, []byte, error) {
	// 1. Generate a CSR
	// Construct the dns id from service name and name space.
	// Example: istio-pilot.istio-system.svc, istio-pilot.istio-system
	id := fmt.Sprintf("%s.%s.svc", svcName, secretNamespace)
	id += "," + fmt.Sprintf("%s.%s", svcName, secretNamespace)
	options := util.CertOptions{
		Host:       id,
		RSAKeySize: keySize,
		IsDualUse:  false,
		PKCS8Key:   false,
	}
	csrPEM, keyPEM, err := util.GenCSR(options)
	if err != nil {
		log.Errorf("CSR generation error (%v)", err)
		return nil, nil, err
	}

	// 2. Submit a CSR
	csrName := fmt.Sprintf("domain-%s-ns-%s-secret-%s", spiffe.GetTrustDomain(), secretNamespace, secretName)
	k8sCSR := &cert.CertificateSigningRequest{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "certificates.k8s.io/v1beta1",
			Kind:       "CertificateSigningRequest",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: csrName,
		},
		Spec: cert.CertificateSigningRequestSpec{
			Request: csrPEM,
			Groups:  []string{"system:authenticated"},
			Usages: []cert.KeyUsage{
				cert.UsageDigitalSignature,
				cert.UsageKeyEncipherment,
				cert.UsageServerAuth,
				cert.UsageClientAuth,
			},
		},
	}
	r, err := wc.certClient.CertificateSigningRequests().Create(k8sCSR)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			log.Debugf("failed to create CSR (%v): %v", csrName, err)
			return nil, nil, err
		}
		//Otherwise, delete the existing CSR and create again
		log.Debugf("delete an existing CSR: %v", csrName)
		err = wc.certClient.CertificateSigningRequests().Delete(csrName, nil)
		if err != nil {
			log.Errorf("failed to delete CSR (%v): %v", csrName, err)
			return nil, nil, err
		}
		log.Debugf("create CSR (%v) after the existing one was deleted", csrName)
		r, err = wc.certClient.CertificateSigningRequests().Create(k8sCSR)
		if err != nil {
			log.Debugf("failed to create CSR (%v): %v", csrName, err)
			return nil, nil, err
		}
	}
	log.Debugf("CSR (%v) is created: %v", csrName, r)

	// 3. Approve a CSR
	log.Debugf("approve CSR (%v) ...", csrName)
	r.Status.Conditions = append(r.Status.Conditions, cert.CertificateSigningRequestCondition{
		Type:    cert.CertificateApproved,
		Reason:  "k8s CSR is approved",
		Message: "The CSR is approved",
	})
	reqApproval, err := wc.certClient.CertificateSigningRequests().UpdateApproval(r)
	if err != nil {
		log.Debugf("failed to approve CSR (%v): %v", csrName, err)
		errCsr := wc.cleanUpCertGen(csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, err
	}
	log.Debugf("CSR (%v) is approved: %v", csrName, reqApproval)

	// 4. Read the signed certificate
	var reqSigned *cert.CertificateSigningRequest
	for i := 0; i < maxNumCertRead; i++ {
		time.Sleep(certReadInterval)
		reqSigned, err = wc.certClient.CertificateSigningRequests().Get(csrName, metav1.GetOptions{})
		if err != nil {
			log.Errorf("failed to get the CSR (%v): %v", csrName, err)
			errCsr := wc.cleanUpCertGen(csrName)
			if errCsr != nil {
				log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
			}
			return nil, nil, err
		}
		if reqSigned.Status.Certificate != nil {
			// Certificate is ready
			break
		}
	}
	var certPEM []byte
	if reqSigned.Status.Certificate != nil {
		log.Debugf("the length of the certificate is %v", len(reqSigned.Status.Certificate))
		log.Debugf("the certificate for CSR (%v) is: %v", csrName, string(reqSigned.Status.Certificate))
		certPEM = reqSigned.Status.Certificate
	} else {
		log.Errorf("failed to read the certificate for CSR (%v)", csrName)
		// Output the first CertificateDenied condition, if any, in the status
		for _, c := range r.Status.Conditions {
			if c.Type == cert.CertificateDenied {
				log.Errorf("CertificateDenied, name: %v, uid: %v, cond-type: %v, cond: %s",
					r.Name, r.UID, c.Type, c.String())
				break
			}
		}
		errCsr := wc.cleanUpCertGen(csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, fmt.Errorf("failed to read the certificate for CSR (%v)", csrName)
	}
	caCert := wc.getCurCACert()
	// Verify the certificate chain before returning the certificate
	roots := x509.NewCertPool()
	if roots == nil {
		errCsr := wc.cleanUpCertGen(csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, fmt.Errorf("failed to create cert pool")
	}
	if ok := roots.AppendCertsFromPEM(caCert); !ok {
		errCsr := wc.cleanUpCertGen(csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, fmt.Errorf("failed to append CA certificate")
	}
	certParsed, err := util.ParsePemEncodedCertificate(certPEM)
	if err != nil {
		log.Errorf("failed to parse the certificate: %v", err)
		errCsr := wc.cleanUpCertGen(csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, fmt.Errorf("failed to parse the certificate: %v", err)
	}
	_, err = certParsed.Verify(x509.VerifyOptions{
		Roots: roots,
	})
	if err != nil {
		log.Errorf("failed to verify the certificate chain: %v", err)
		errCsr := wc.cleanUpCertGen(csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, fmt.Errorf("failed to verify the certificate chain: %v", err)
	}
	certChain := []byte{}
	certChain = append(certChain, certPEM...)
	certChain = append(certChain, caCert...)

	// 5. Clean up the artifacts (e.g., delete CSR)
	err = wc.cleanUpCertGen(csrName)
	if err != nil {
		log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
	}
	// If there is a failure of cleaning up CSR, the error is returned.
	return certChain, keyPEM, err
}

func readCACert() ([]byte, error) {
	caCert, err := ioutil.ReadFile(caCertPath)
	if err != nil {
		log.Errorf("failed to read CA cert, cert. path: %v, error: %v", caCertPath, err)
		return nil, fmt.Errorf("failed to read CA cert, cert. path: %v, error: %v", caCertPath, err)
	}
	return caCert, nil
}

func doPatch(client *kubernetes.Clientset, webhookConfigName, webhookName string, caCertPem []byte) {
	if err := istioutil.PatchMutatingWebhookConfig(client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
		webhookConfigName, webhookName, caCertPem); err != nil {
		log.Errorf("Patch webhook failed: %v", err)
	}
}

func isTCPReachable(host string, port int) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		log.Debugf("DialTimeout() returns err: %v", err)
		// No connection yet, so no need to conn.Close()
		return false
	}
	defer conn.Close()
	return true
}
