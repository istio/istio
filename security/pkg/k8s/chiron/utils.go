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

package chiron

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net"
	"time"

	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"

	cert "k8s.io/api/certificates/v1beta1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	rand "k8s.io/apimachinery/pkg/util/rand"
	certclient "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
)

const (
	maxNameLength       = 63
	maxDomainNameLength = 6
	maxNamespaceLength  = 8
	maxSecretNameLength = 8
	randomLength        = 18
)

// getRandomCsrName returns a random name for CSR.
func getRandomCsrName(secretName, namespace string) string {
	domain := spiffe.GetTrustDomain()
	if len(domain) > maxDomainNameLength {
		domain = domain[:maxDomainNameLength]
	}
	if len(namespace) > maxNamespaceLength {
		namespace = namespace[:maxNamespaceLength]
	}
	if len(secretName) > maxSecretNameLength {
		secretName = secretName[:maxSecretNameLength]
	}
	name := fmt.Sprintf("csr-%s-%s-%s-%s",
		domain, namespace, secretName, rand.String(randomLength))
	if len(name) > maxNameLength {
		name = name[:maxNameLength]
	}
	return name
}

// GenKeyCertK8sCA generates a certificate and key from k8s CA
// Working flow:
// 1. Generate a CSR
// 2. Submit a CSR
// 3. Approve a CSR
// 4. Read the signed certificate
// 5. Clean up the artifacts (e.g., delete CSR)
func GenKeyCertK8sCA(certClient certclient.CertificateSigningRequestInterface, dnsName,
	secretName, secretNamespace, caFilePath string) ([]byte, []byte, []byte, error) {
	// 1. Generate a CSR
	options := util.CertOptions{
		Host:       dnsName,
		RSAKeySize: keySize,
		IsDualUse:  false,
		PKCS8Key:   false,
	}
	csrPEM, keyPEM, err := util.GenCSR(options)
	if err != nil {
		log.Errorf("CSR generation error (%v)", err)
		return nil, nil, nil, err
	}

	// 2. Submit the CSR
	csrName := getRandomCsrName(secretName, secretNamespace)
	numRetries := 3
	r, err := submitCSR(certClient, csrName, csrPEM, numRetries)
	if err != nil {
		return nil, nil, nil, err
	}
	if r == nil {
		return nil, nil, nil, fmt.Errorf("the CSR returned is nil")
	}

	// 3. Approve a CSR
	log.Debugf("approve CSR (%v) ...", csrName)
	csrMsg := fmt.Sprintf("CSR (%s) for the certificate (%s) is approved", csrName, dnsName)
	r.Status.Conditions = append(r.Status.Conditions, cert.CertificateSigningRequestCondition{
		Type:    cert.CertificateApproved,
		Reason:  csrMsg,
		Message: csrMsg,
	})
	reqApproval, err := certClient.UpdateApproval(context.TODO(), r, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("failed to approve CSR (%v): %v", csrName, err)
		errCsr := cleanUpCertGen(certClient, csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, nil, err
	}
	log.Debugf("CSR (%v) is approved: %v", csrName, reqApproval)

	// 4. Read the signed certificate
	certChain, caCert, err := readSignedCertificate(certClient,
		csrName, certReadInterval, certWatchTimeout, maxNumCertRead, caFilePath)
	if err != nil {
		log.Errorf("failed to read signed cert. (%v): %v", csrName, err)
		errCsr := cleanUpCertGen(certClient, csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, nil, err
	}

	// 5. Clean up the artifacts (e.g., delete CSR)
	err = cleanUpCertGen(certClient, csrName)
	if err != nil {
		log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
	}
	// If there is a failure of cleaning up CSR, the error is returned.
	return certChain, keyPEM, caCert, err
}

// Read CA certificate and check whether it is a valid certificate.
func readCACert(caCertPath string) ([]byte, error) {
	caCert, err := ioutil.ReadFile(caCertPath)
	if err != nil {
		log.Errorf("failed to read CA cert, cert. path: %v, error: %v", caCertPath, err)
		return nil, fmt.Errorf("failed to read CA cert, cert. path: %v, error: %v", caCertPath, err)
	}

	b, _ := pem.Decode(caCert)
	if b == nil {
		return nil, fmt.Errorf("could not decode pem")
	}
	if b.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("ca certificate contains wrong type: %v", b.Type)
	}
	if _, err := x509.ParseCertificate(b.Bytes); err != nil {
		return nil, fmt.Errorf("ca certificate parsing returns an error: %v", err)
	}

	return caCert, nil
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

// Reload CA cert from file and return whether CA cert is changed
func reloadCACert(wc *WebhookController) (bool, error) {
	certChanged := false
	wc.certMutex.Lock()
	defer wc.certMutex.Unlock()
	caCert, err := readCACert(wc.k8sCaCertFile)
	if err != nil {
		return certChanged, err
	}
	if !bytes.Equal(caCert, wc.CACert) {
		wc.CACert = append([]byte(nil), caCert...)
		certChanged = true
	}
	return certChanged, nil
}

func submitCSR(certClient certclient.CertificateSigningRequestInterface, csrName string,
	csrPEM []byte, numRetries int) (*cert.CertificateSigningRequest, error) {
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
	var reqRet *cert.CertificateSigningRequest
	var errRet error
	for i := 0; i < numRetries; i++ {
		log.Debugf("trial %v to create CSR (%v)", i, csrName)
		reqRet, errRet = certClient.Create(context.TODO(), k8sCSR, metav1.CreateOptions{})
		if errRet == nil && reqRet != nil {
			break
		}
		// If an err other than the CSR exists is returned, re-try
		if !kerrors.IsAlreadyExists(errRet) {
			log.Debugf("failed to create CSR (%v): %v", csrName, errRet)
			continue
		}
		// If CSR exists, delete the existing CSR and create again
		log.Debugf("delete an existing CSR: %v", csrName)
		errRet = certClient.Delete(context.TODO(), csrName, metav1.DeleteOptions{})
		if errRet != nil {
			log.Errorf("failed to delete CSR (%v): %v", csrName, errRet)
			continue
		}
		log.Debugf("create CSR (%v) after the existing one was deleted", csrName)
		reqRet, errRet = certClient.Create(context.TODO(), k8sCSR, metav1.CreateOptions{})
		if errRet == nil && reqRet != nil {
			break
		}
	}
	return reqRet, errRet
}

// Read the signed certificate
func readSignedCertificate(certClient certclient.CertificateSigningRequestInterface, csrName string,
	readInterval, watchTimeout time.Duration, maxNumRead int, caCertPath string) ([]byte, []byte, error) {
	// First try to read the signed CSR through a watching mechanism
	reqSigned := readSignedCsr(certClient, csrName, watchTimeout)
	if reqSigned == nil {
		// If watching fails, retry reading the signed CSR a few times after waiting.
		for i := 0; i < maxNumRead; i++ {
			r, err := certClient.Get(context.TODO(), csrName, metav1.GetOptions{})
			if err != nil {
				log.Errorf("failed to get the CSR (%v): %v", csrName, err)
				errCsr := cleanUpCertGen(certClient, csrName)
				if errCsr != nil {
					log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
				}
				return nil, nil, err
			}
			if r.Status.Certificate != nil {
				// Certificate is ready
				reqSigned = r
				break
			}
			time.Sleep(readInterval)
		}
	}
	// If still failed to read signed CSR, return error.
	if reqSigned == nil {
		log.Errorf("failed to read the certificate for CSR (%v), nil CSR", csrName)
		errCsr := cleanUpCertGen(certClient, csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, errCsr)
		}
		return nil, nil, fmt.Errorf("failed to read the certificate for CSR (%v), nil CSR", csrName)
	}
	if reqSigned.Status.Certificate == nil {
		log.Errorf("failed to read the certificate for CSR (%v), nil cert", csrName)
		// Output the first CertificateDenied condition, if any, in the status
		for _, c := range reqSigned.Status.Conditions {
			if c.Type == cert.CertificateDenied {
				log.Errorf("CertificateDenied, name: %v, uid: %v, cond-type: %v, cond: %s",
					reqSigned.Name, reqSigned.UID, c.Type, c.String())
				break
			}
		}
		errCsr := cleanUpCertGen(certClient, csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, errCsr)
		}
		return nil, nil, fmt.Errorf("failed to read the certificate for CSR (%v), nil cert", csrName)
	}

	log.Debugf("the length of the certificate is %v", len(reqSigned.Status.Certificate))
	log.Debugf("the certificate for CSR (%v) is: %v", csrName, string(reqSigned.Status.Certificate))

	certPEM := reqSigned.Status.Certificate
	caCert, err := readCACert(caCertPath)
	if err != nil {
		log.Errorf("error when getting CA cert (%v)", err)
		errCsr := cleanUpCertGen(certClient, csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, err
	}
	// Verify the certificate chain before returning the certificate
	roots := x509.NewCertPool()
	if roots == nil {
		errCsr := cleanUpCertGen(certClient, csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, fmt.Errorf("failed to create cert pool")
	}
	if ok := roots.AppendCertsFromPEM(caCert); !ok {
		errCsr := cleanUpCertGen(certClient, csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, fmt.Errorf("failed to append CA certificate")
	}
	certParsed, err := util.ParsePemEncodedCertificate(certPEM)
	if err != nil {
		log.Errorf("failed to parse the certificate: %v", err)
		errCsr := cleanUpCertGen(certClient, csrName)
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
		errCsr := cleanUpCertGen(certClient, csrName)
		if errCsr != nil {
			log.Errorf("failed to clean up CSR (%v): %v", csrName, err)
		}
		return nil, nil, fmt.Errorf("failed to verify the certificate chain: %v", err)
	}
	certChain := []byte{}
	certChain = append(certChain, certPEM...)
	certChain = append(certChain, caCert...)

	return certChain, caCert, nil
}

// Return signed CSR through a watcher. If no CSR is read, return nil.
// The following nonlint is to fix the lint error: `certClient` can be `k8s.io/client-go/tools/cache.Watcher` (interfacer)
// nolint: interfacer
func readSignedCsr(certClient certclient.CertificateSigningRequestInterface, csrName string, timeout time.Duration) *cert.CertificateSigningRequest {
	watcher, err := certClient.Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", csrName).String(),
	})
	if err != nil {
		log.Errorf("err when watching CSR %v: %v", csrName, err)
		return nil
	}
	// Set a timeout
	timer := time.After(timeout)
	for {
		select {
		case r := <-watcher.ResultChan():
			reqSigned := r.Object.(*cert.CertificateSigningRequest)
			if reqSigned.Status.Certificate != nil {
				return reqSigned
			}
		case <-timer:
			log.Errorf("timeout when watching CSR %v", csrName)
			return nil
		}
	}
}

// Clean up the CSR
func cleanUpCertGen(certClient certclient.CertificateSigningRequestInterface, csrName string) error {
	// Delete CSR
	log.Debugf("delete CSR: %v", csrName)
	err := certClient.Delete(context.TODO(), csrName, metav1.DeleteOptions{})
	if err != nil {
		log.Errorf("failed to delete CSR (%v): %v", csrName, err)
		return err
	}
	return nil
}
